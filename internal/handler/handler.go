package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
	"YALS/internal/executor"
	"YALS/internal/logger"
	"YALS/internal/utils"
	"YALS/internal/validator"

	"github.com/gorilla/websocket"
)

type Handler struct {
	server          *config.ServerInfo
	executor        *executor.Executor
	upgrader        websocket.Upgrader
	clients         map[*websocket.Conn]bool
	clientIPs       map[*websocket.Conn]string
	clientSessions  map[*websocket.Conn]string
	sessionConns    map[string]*websocket.Conn
	commandSessions map[string]string
	clientsLock     sync.RWMutex
	pingInterval    time.Duration
	pongWait        time.Duration
	webDir          string
	rateLimiter     *RateLimiter
}

type RateLimiter struct {
	enabled     bool
	maxCommands int
	timeWindow  time.Duration
	sessions    map[string]*SessionRateLimit
	mu          sync.RWMutex
}

type SessionRateLimit struct {
	timestamps []time.Time
}

type CommandRequest struct {
	Type      string `json:"type"`
	Host      string `json:"host,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Host    string `json:"host"`
	Command string `json:"command"`
	Target  string `json:"target"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type StreamingCommandResponse struct {
	Type       string `json:"type"`
	Success    bool   `json:"success"`
	Host       string `json:"host"`
	Command    string `json:"command"`
	Target     string `json:"target"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	IsComplete bool   `json:"is_complete"`
	CommandID  string `json:"command_id,omitempty"`
}

type CommandsListResponse struct {
	Type     string                    `json:"type"`
	Commands []validator.CommandDetail `json:"commands"`
}

type CommandTemplate struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	Description  string `json:"description"`
	IgnoreTarget bool   `json:"ignore_target"`
	MaximumQueue int    `json:"maximum_queue"`
}

type AppConfigResponse struct {
	Type     string                 `json:"type"`
	Version  string                 `json:"version"`
	Host     map[string]interface{} `json:"host"`
	Commands []CommandTemplate      `json:"commands"`
}

type SessionIDResponse struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

func NewHandler(serverInstance *config.ServerInfo, executor *executor.Executor, pingInterval, pongWait time.Duration) *Handler {
	cfg := config.GetConfig()

	rateLimiter := &RateLimiter{
		enabled:     cfg.RateLimit.Enabled,
		maxCommands: cfg.RateLimit.MaxCommands,
		timeWindow:  time.Duration(cfg.RateLimit.TimeWindow) * time.Second,
		sessions:    make(map[string]*SessionRateLimit),
	}

	return &Handler{
		server:   serverInstance,
		executor: executor,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  65536,
			WriteBufferSize: 65536,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		clients:         make(map[*websocket.Conn]bool),
		clientIPs:       make(map[*websocket.Conn]string),
		clientSessions:  make(map[*websocket.Conn]string),
		sessionConns:    make(map[string]*websocket.Conn),
		commandSessions: make(map[string]string),
		pingInterval:    pingInterval,
		pongWait:        pongWait,
		rateLimiter:     rateLimiter,
	}
}

func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/session", h.handleGetSession)
	mux.HandleFunc("/ws/", h.handleWebSocket)

	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		indexPath := filepath.Join(h.webDir, "index.html")
		http.ServeFile(w, r, indexPath)
		return
	default:
		filePath := filepath.Join(h.webDir, r.URL.Path[1:])
		if _, err := http.Dir(h.webDir).Open(r.URL.Path[1:]); err == nil {
			http.ServeFile(w, r, filePath)
			return
		}

		if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "application/json") {
			indexPath := filepath.Join(h.webDir, "index.html")
			http.ServeFile(w, r, indexPath)
			return
		}

		http.NotFound(w, r)
		return
	}
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	clientIP := h.getRealIP(r)

	var sessionID string
	path := strings.TrimPrefix(r.URL.Path, "/ws/")

	if path != "" && path != r.URL.Path {
		sessionID = path
	} else {
		sessionID = r.URL.Query().Get("sessionId")
	}

	if sessionID == "" {
		sessionID = h.generateSessionID()
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}

	h.clientsLock.Lock()
	h.clients[conn] = true
	h.clientIPs[conn] = clientIP
	h.clientSessions[conn] = sessionID
	h.sessionConns[sessionID] = conn
	h.clientsLock.Unlock()

	conn.SetReadLimit(32768)
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	sessionResponse := SessionIDResponse{
		Type:      "session_id",
		SessionID: sessionID,
	}
	if err := conn.WriteJSON(sessionResponse); err != nil {
		logger.Errorf("Failed to send session ID: %v", err)
		conn.Close()
		return
	}

	go h.pingClient(conn)
	go h.readPump(conn, clientIP)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := h.generateSessionID()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := SessionIDResponse{
		Type:      "session_id",
		SessionID: sessionID,
	}
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) getRealIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}

	return r.RemoteAddr
}

// GenerateRandomString generates a random alphanumeric string of specified length
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (h *Handler) generateSessionID() string {
	timestamp := time.Now().UnixMilli() // Get millisecond timestamp
	randomStr := GenerateRandomString(10)
	return fmt.Sprintf("session_%d_%s", timestamp, randomStr)
}

func (h *Handler) pingClient(conn *websocket.Conn) {
	ticker := time.NewTicker(h.pingInterval)
	defer func() {
		ticker.Stop()
		conn.Close()

		h.clientsLock.Lock()
		sessionID := h.clientSessions[conn]
		delete(h.clients, conn)
		delete(h.clientIPs, conn)
		delete(h.clientSessions, conn)
		if sessionID != "" {
			delete(h.sessionConns, sessionID)
		}
		h.clientsLock.Unlock()
	}()

	for range ticker.C {
		if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
			return
		}
	}
}

func (h *Handler) readPump(conn *websocket.Conn, clientIP string) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("WebSocket error: %v", err)
			}
			break
		}

		var req CommandRequest
		if err := json.Unmarshal(message, &req); err != nil {
			logger.Errorf("Failed to parse command request: %v", err)
			continue
		}

		logger.Debugf("Received message type: %s, CommandID: %s", req.Type, req.CommandID)

		switch req.Type {
		case "get_commands":
			h.handleGetCommands(conn)
		case "get_config":
			h.handleGetConfig(conn)
		case "execute_command":
			go h.handleCommand(conn, req, clientIP)
		case "stop_command":
			h.handleStopCommand(req, clientIP)
		default:
			logger.Warnf("Unknown message type: %s", req.Type)
		}
	}
}

func (h *Handler) handleCommand(conn *websocket.Conn, req CommandRequest, clientIP string) {
	resp := h.createCommandResponse(req, false)

	h.clientsLock.RLock()
	sessionID := h.clientSessions[conn]
	h.clientsLock.RUnlock()

	if !h.rateLimiter.checkRateLimit(sessionID) {
		remaining := h.rateLimiter.getRemainingTime(sessionID)
		resp.Success = false
		resp.Error = fmt.Sprintf("Rate limit exceeded. Please wait %d seconds before trying again.", int(remaining.Seconds())+1)
		h.sendStreamingResponse(conn, resp, true, "", "replace", false)
		logger.Warnf("Client [%s] rate limit exceeded for session: %s", clientIP, sessionID)
		return
	}

	cmdConfig, exists := h.server.GetCommandConfig(req.Command)
	if !exists {
		resp.Success = false
		resp.Error = "Command not found: " + req.Command
		h.sendStreamingResponse(conn, resp, true, "", "replace", false)
		return
	}

	if !cmdConfig.IgnoreTarget {
		inputType := validator.ValidateInput(req.Target)
		if inputType == validator.InvalidInput {
			resp.Success = false
			resp.Error = "Invalid target: must be an IP address or domain name"
			h.sendStreamingResponse(conn, resp, true, "", "replace", false)
			return
		}
	}

	outputChan := make(chan executor.Output, 100)
	commandID := h.executor.Execute(req.Command, req.Target, sessionID, outputChan)

	if commandID == "" {
		resp.Success = false
		resp.Error = "Failed to execute command"
		h.sendStreamingResponse(conn, resp, true, "", "replace", false)
		return
	}

	h.clientsLock.Lock()
	h.commandSessions[commandID] = sessionID
	h.clientsLock.Unlock()

	defer func() {
		h.clientsLock.Lock()
		delete(h.commandSessions, commandID)
		h.clientsLock.Unlock()
	}()

	logger.Infof("Client [%s] sent run signal for command: %s", clientIP, commandID)

	// Send command_id immediately to frontend
	resp.Success = true
	h.sendStreamingResponse(conn, resp, false, commandID, "replace", false)

	for output := range outputChan {
		if output.IsStopped {
			stoppedResp := h.createCommandResponse(req, true)
			stoppedResp.Output = "\n*** Stopped ***"
			h.sendStreamingResponse(conn, stoppedResp, true, commandID, "append", true)
			break
		}

		if output.IsComplete {
			// Send completion message even if there's no output
			if output.Output != "" {
				resp.Success = true
				resp.Output = output.Output
				h.sendStreamingResponse(conn, resp, true, commandID, "replace", false)
			} else {
				// Send empty completion message
				resp.Success = true
				resp.Output = ""
				h.sendStreamingResponse(conn, resp, true, commandID, "replace", false)
			}
			break
		}

		if output.IsError && output.Output != "" {
			resp.Success = false
			resp.Error = output.Output
			h.sendStreamingResponse(conn, resp, false, commandID, "replace", false)
		} else if output.Output != "" {
			resp.Success = true
			resp.Output = output.Output
			h.sendStreamingResponse(conn, resp, false, commandID, "replace", false)
		}
	}
}

func (h *Handler) handleGetCommands(conn *websocket.Conn) {
	commands := h.server.GetCommands()
	response := CommandsListResponse{
		Type:     "commands_list",
		Commands: h.convertToCommandDetails(commands),
	}

	data, err := json.Marshal(response)
	if err != nil {
		logger.Errorf("Failed to marshal commands: %v", err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	if _, ok := h.clients[conn]; ok {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (h *Handler) convertToCommandDetails(commands []config.CommandInfo) []validator.CommandDetail {
	details := make([]validator.CommandDetail, len(commands))
	for i, cmd := range commands {
		details[i] = validator.CommandDetail{
			Name:         cmd.Name,
			Description:  cmd.Description,
			IgnoreTarget: cmd.IgnoreTarget,
		}
	}
	return details
}

func (h *Handler) handleGetConfig(conn *websocket.Conn) {
	cfg := config.GetConfig()
	if cfg == nil {
		logger.Errorf("Configuration not available")
		return
	}

	commands := h.server.GetCommands()

	commandsSlice := make([]CommandTemplate, 0, len(commands))
	for _, cmd := range commands {
		commandsSlice = append(commandsSlice, CommandTemplate{
			Name:         cmd.Name,
			Template:     cmd.Template,
			Description:  cmd.Description,
			IgnoreTarget: cmd.IgnoreTarget,
			MaximumQueue: cmd.MaximumQueue,
		})
	}

	info := h.server.GetInfo()

	response := AppConfigResponse{
		Type:     "app_config",
		Version:  utils.GetAppVersion(),
		Host:     info,
		Commands: commandsSlice,
	}

	if err := conn.WriteJSON(response); err != nil {
		logger.Errorf("Failed to send app config: %v", err)
	}
}

func (h *Handler) handleStopCommand(req CommandRequest, clientIP string) {
	if req.CommandID == "" {
		logger.Warnf("Stop command request missing command_id")
		return
	}

	if h.executor.Stop(req.CommandID) {
		logger.Infof("Client [%s] sent stop signal for command: %s", clientIP, req.CommandID)
	}
}

func (h *Handler) sendStreamingResponse(conn *websocket.Conn, resp CommandResponse, isComplete bool, commandID string, outputMode string, stopped bool) {
	streamResp := map[string]any{
		"type":        "command_output",
		"success":     resp.Success,
		"host":        resp.Host,
		"command":     resp.Command,
		"target":      resp.Target,
		"output":      resp.Output,
		"error":       resp.Error,
		"is_complete": isComplete,
		"command_id":  commandID,
		"output_mode": outputMode,
		"stopped":     stopped,
	}

	data, err := json.Marshal(streamResp)
	if err != nil {
		logger.Errorf("Failed to marshal streaming response: %v", err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	if _, ok := h.clients[conn]; ok {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (h *Handler) createCommandResponse(req CommandRequest, success bool) CommandResponse {
	return CommandResponse{
		Success: success,
		Host:    "localhost",
		Command: req.Command,
		Target:  req.Target,
	}
}

func (rl *RateLimiter) checkRateLimit(sessionID string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.RLock()
	session, exists := rl.sessions[sessionID]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		rl.sessions[sessionID] = &SessionRateLimit{
			timestamps: make([]time.Time, 0),
		}
		rl.mu.Unlock()
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	session.timestamps = filterRecentTimestamps(session.timestamps, now, rl.timeWindow)

	if len(session.timestamps) >= rl.maxCommands {
		return false
	}

	session.timestamps = append(session.timestamps, now)
	return true
}

func (rl *RateLimiter) getRemainingTime(sessionID string) time.Duration {
	rl.mu.RLock()
	session, exists := rl.sessions[sessionID]
	rl.mu.RUnlock()

	if !exists {
		return 0
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	session.timestamps = filterRecentTimestamps(session.timestamps, now, rl.timeWindow)

	if len(session.timestamps) == 0 {
		return 0
	}

	oldest := session.timestamps[0]
	elapsed := now.Sub(oldest)
	remaining := rl.timeWindow - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

func filterRecentTimestamps(timestamps []time.Time, now time.Time, window time.Duration) []time.Time {
	var result []time.Time
	for _, t := range timestamps {
		if now.Sub(t) < window {
			result = append(result, t)
		}
	}
	return result
}
