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
)

type Handler struct {
	server         *config.ServerInfo
	executor       *executor.Executor
	activeCommands map[string]chan bool
	commandsLock   sync.RWMutex
	webDir         string
	rateLimiter    *RateLimiter
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
	Agent     string `json:"agent,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
	IPVersion string `json:"ip_version,omitempty"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Target  string `json:"target"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type StreamingCommandResponse struct {
	Type       string `json:"type"`
	Success    bool   `json:"success"`
	Agent      string `json:"agent"`
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
	IgnoreTarget bool   `json:"ignore_target"`
}

type AppConfigResponse struct {
	Type     string                 `json:"type"`
	Version  string                 `json:"version"`
	Host     map[string]interface{} `json:"host"`
	Commands []CommandTemplate      `json:"commands"`
}

type SessionResponse struct {
	SessionID string `json:"session_id"`
}

type NodeResponse struct {
	Version     string           `json:"version"`
	TotalNodes  int              `json:"total_nodes"`
	OnlineNodes int              `json:"online_nodes"`
	Groups      []map[string]any `json:"groups"`
}

type ExecRequest struct {
	Agent     string `json:"agent"`
	Command   string `json:"command"`
	Target    string `json:"target"`
	IPVersion string `json:"ip_version"`
}

type StopRequest struct {
	CommandID string `json:"command_id"`
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
		server:         serverInstance,
		executor:       executor,
		activeCommands: make(map[string]chan bool),
		rateLimiter:    rateLimiter,
	}
}

func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/session", h.handleGetSession)
	mux.HandleFunc("/api/node", h.handleGetNodes)
	mux.HandleFunc("/api/exec", h.handleExecCommand)
	mux.HandleFunc("/api/stop", h.handleStopCommand)

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

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.generateSessionID()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	response := SessionResponse{
		SessionID: sessionID,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode session response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) handleGetNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	commands := h.server.GetCommands()
	commandsSlice := make([]CommandTemplate, 0, len(commands))
	for _, cmd := range commands {
		commandsSlice = append(commandsSlice, CommandTemplate{
			Name:         cmd.Name,
			IgnoreTarget: cmd.IgnoreTarget,
		})
	}

	info := h.server.GetInfo()

	response := AppConfigResponse{
		Type:     "app_config",
		Version:  utils.GetAppVersion(),
		Host:     info,
		Commands: commandsSlice,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode nodes response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) handleExecCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	clientIP := h.getRealIP(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	if !h.rateLimiter.checkRateLimit(sessionID) {
		remaining := h.rateLimiter.getRemainingTime(sessionID)
		errorMsg := fmt.Sprintf("Rate limit exceeded. Please wait %d seconds before trying again.", int(remaining.Seconds())+1)
		h.sendSSEError(w, flusher, errorMsg)
		logger.Warnf("Client [%s] rate limit exceeded for session: %s", clientIP, sessionID)
		return
	}

	cmdConfig, exists := h.server.GetCommandConfig(req.Command)
	if !exists {
		h.sendSSEError(w, flusher, "Command not found: "+req.Command)
		return
	}

	if !cmdConfig.IgnoreTarget {
		inputType := validator.ValidateInput(req.Target)
		if inputType == validator.InvalidInput {
			h.sendSSEError(w, flusher, "Invalid target: must be an IP address or domain name")
			return
		}
	}

	ipVersion := req.IPVersion
	if ipVersion == "" {
		ipVersion = "auto"
	}

	outputChan := make(chan executor.Output, 100)
	commandID := h.executor.ExecuteWithIPVersion(req.Command, req.Target, sessionID, ipVersion, outputChan)

	if commandID == "" {
		h.sendSSEError(w, flusher, "Failed to execute command")
		return
	}

	stopChan := make(chan bool, 1)
	h.setActiveCommand(commandID, stopChan)
	defer h.removeActiveCommand(commandID)

	logger.Infof("Client [%s] executing command: %s", clientIP, commandID)

	h.sendSSEMessage(w, flusher, map[string]any{
		"type":       "output",
		"command_id": commandID,
		"success":    true,
	})

	for output := range outputChan {
		if output.IsStopped {
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":    "output",
				"output":  "\n*** Stopped ***",
				"stopped": true,
			})
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":    "complete",
				"success": false,
				"stopped": true,
			})
			break
		}

		if output.IsComplete {
			if output.IsError {
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":    "complete",
					"success": false,
					"error":   output.Error,
				})
			} else {
				if output.Output != "" {
					h.sendSSEMessage(w, flusher, map[string]any{
						"type":   "output",
						"output": output.Output,
					})
				}
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":    "complete",
					"success": true,
				})
			}
			break
		}

		if output.IsError {
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":  "error",
				"error": output.Error,
			})
		} else {
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":   "output",
				"output": output.Output,
			})
		}
	}
}

func (h *Handler) handleStopCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.CommandID == "" {
		http.Error(w, "Missing command_id", http.StatusBadRequest)
		return
	}

	clientIP := h.getRealIP(r)

	if h.stopActiveCommand(req.CommandID) {
		logger.Infof("Client [%s] sent stop signal for command: %s", clientIP, req.CommandID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Command stopped",
		})
	} else {
		logger.Warnf("Client [%s] attempted to stop non-existent command: %s", clientIP, req.CommandID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "Command not found or already completed",
		})
	}
}

func (h *Handler) sendSSEMessage(w http.ResponseWriter, flusher http.Flusher, data map[string]any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Errorf("Failed to marshal SSE message: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

func (h *Handler) sendSSEError(w http.ResponseWriter, flusher http.Flusher, errorMsg string) {
	h.sendSSEMessage(w, flusher, map[string]any{
		"type":    "complete",
		"success": false,
		"error":   errorMsg,
	})
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

func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

func (h *Handler) generateSessionID() string {
	timestamp := time.Now().UnixMilli()
	randomStr := GenerateRandomString(10)
	return fmt.Sprintf("session_%d_%s", timestamp, randomStr)
}

func (h *Handler) validateSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if !strings.HasPrefix(sessionID, "session_") {
		return false
	}
	return true
}

func (h *Handler) setActiveCommand(commandID string, stopChan chan bool) {
	h.commandsLock.Lock()
	h.activeCommands[commandID] = stopChan
	h.commandsLock.Unlock()
}

func (h *Handler) removeActiveCommand(commandID string) {
	h.commandsLock.Lock()
	delete(h.activeCommands, commandID)
	h.commandsLock.Unlock()
}

func (h *Handler) stopActiveCommand(commandID string) bool {
	h.commandsLock.Lock()
	defer h.commandsLock.Unlock()

	if stopChan, exists := h.activeCommands[commandID]; exists {
		close(stopChan)
		delete(h.activeCommands, commandID)
		return true
	}
	return false
}

func (rl *RateLimiter) checkRateLimit(sessionID string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	if _, exists := rl.sessions[sessionID]; !exists {
		rl.sessions[sessionID] = &SessionRateLimit{
			timestamps: []time.Time{},
		}
	}

	session := rl.sessions[sessionID]

	validTimestamps := []time.Time{}
	for _, ts := range session.timestamps {
		if now.Sub(ts) < rl.timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	session.timestamps = validTimestamps

	if len(session.timestamps) >= rl.maxCommands {
		return false
	}

	session.timestamps = append(session.timestamps, now)
	return true
}

func (rl *RateLimiter) getRemainingTime(sessionID string) time.Duration {
	if !rl.enabled {
		return 0
	}

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	session, exists := rl.sessions[sessionID]
	if !exists || len(session.timestamps) == 0 {
		return 0
	}

	oldestTimestamp := session.timestamps[0]
	elapsed := time.Since(oldestTimestamp)
	remaining := rl.timeWindow - elapsed

	if remaining < 0 {
		return 0
	}
	return remaining
}
