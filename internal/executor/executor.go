package executor

import (
	"YALS/internal/config"
	"YALS/internal/validator"
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var shellOperators = []string{"|", "&&", "||", ">", "<", ";"}

type Output struct {
	Output     string
	Error      string
	IsError    bool
	IsComplete bool
	IsStopped  bool
}

type Executor struct {
	config         *config.Config
	activeCommands map[string]*ActiveCommand
	commandsLock   sync.RWMutex
	stopSignals    map[string]chan bool
	stopLock       sync.RWMutex
}

type ActiveCommand struct {
	Cmd         *exec.Cmd
	FullCommand string
}

func NewExecutor(cfg *config.Config) *Executor {
	return &Executor{
		config:         cfg,
		activeCommands: make(map[string]*ActiveCommand),
		stopSignals:    make(map[string]chan bool),
	}
}

func (e *Executor) Execute(commandName, target, sessionID string, outputChan chan<- Output) string {
	return e.ExecuteWithIPVersion(commandName, target, sessionID, "auto", outputChan)
}

// ExecuteWithIPVersion executes a command with IP version preference
func (e *Executor) ExecuteWithIPVersion(commandName, target, sessionID, ipVersion string, outputChan chan<- Output) string {
	cmdConfig, exists := e.config.Commands[commandName]
	if !exists {
		outputChan <- Output{
			Error:      "Command not found: " + commandName,
			IsComplete: true,
			IsError:    true,
		}
		return ""
	}

	// Resolve domain to IP if target is a domain name
	resolvedTarget := target
	if target != "" && !cmdConfig.IgnoreTarget {
		inputType := validator.ValidateInput(target)
		if inputType == validator.Domain {
			// Extract host without port
			host, port := extractHostPort(target)

			// Determine IP version
			var version validator.IPVersion
			switch ipVersion {
			case "ipv4":
				version = validator.IPVersionIPv4
			case "ipv6":
				version = validator.IPVersionIPv6
			default:
				version = validator.IPVersionAuto
			}

			// Resolve domain to IP
			ips, err := validator.ResolveDomainWithVersion(host, version)
			if err != nil {
				outputChan <- Output{
					Error:      fmt.Sprintf("Failed to resolve domain %s: %v", host, err),
					IsComplete: true,
					IsError:    true,
				}
				return ""
			}

			if len(ips) == 0 {
				outputChan <- Output{
					Error:      fmt.Sprintf("No IP addresses found for domain: %s", host),
					IsComplete: true,
					IsError:    true,
				}
				return ""
			}

			// Use the first resolved IP
			resolvedIP := ips[0].String()

			// Reconstruct target with IP and port if present
			if port != "" {
				// Check if it's IPv6 (needs brackets with port)
				if strings.Contains(resolvedIP, ":") {
					resolvedTarget = fmt.Sprintf("[%s]:%s", resolvedIP, port)
				} else {
					resolvedTarget = fmt.Sprintf("%s:%s", resolvedIP, port)
				}
			} else {
				resolvedTarget = resolvedIP
			}

		}
	}

	fullCommand := cmdConfig.Template
	if resolvedTarget != "" && !cmdConfig.IgnoreTarget {
		fullCommand = cmdConfig.Template + " " + resolvedTarget
	}

	commandID := generateCommandID(commandName, target, sessionID)
	stopChan := make(chan bool, 1)

	e.storeCommand(commandID, fullCommand, stopChan)

	go e.runCommand(commandID, fullCommand, stopChan, outputChan)

	return commandID
}

// extractHostPort extracts host and port from target string
func extractHostPort(target string) (host, port string) {
	// Check for IPv6 with port: [2001:db8::1]:8080
	if strings.HasPrefix(target, "[") {
		closeBracket := strings.Index(target, "]")
		if closeBracket == -1 {
			return target, ""
		}
		host = target[1:closeBracket]
		if len(target) > closeBracket+1 && target[closeBracket+1] == ':' {
			port = target[closeBracket+2:]
		}
		return host, port
	}

	// Check for port
	lastColon := strings.LastIndex(target, ":")
	if lastColon == -1 {
		return target, ""
	}

	// Check if it's IPv6 without port
	if strings.Count(target, ":") > 1 {
		return target, ""
	}

	// IPv4 or domain with port
	return target[:lastColon], target[lastColon+1:]
}

func (e *Executor) runCommand(commandID, fullCommand string, stopChan <-chan bool, outputChan chan<- Output) {
	defer func() {
		e.removeCommand(commandID)
		close(outputChan)
	}()

	cmd := e.createCommand(fullCommand)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		outputChan <- Output{
			Error:      "Failed to get stdout pipe: " + err.Error(),
			IsComplete: true,
			IsError:    true,
		}
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		outputChan <- Output{
			Error:      "Failed to get stderr pipe: " + err.Error(),
			IsComplete: true,
			IsError:    true,
		}
		return
	}

	if err := cmd.Start(); err != nil {
		outputChan <- Output{
			Error:      "Failed to start command: " + err.Error(),
			IsComplete: true,
			IsError:    true,
		}
		return
	}

	e.commandsLock.Lock()
	e.activeCommands[commandID] = &ActiveCommand{
		Cmd:         cmd,
		FullCommand: fullCommand,
	}
	e.commandsLock.Unlock()

	done := make(chan error, 1)
	stdoutDone := make(chan bool, 1)
	stderrDone := make(chan bool, 1)
	stopped := make(chan bool, 1)

	go e.streamOutput(stdout, outputChan, stdoutDone, stopped, false)
	go e.streamOutput(stderr, outputChan, stderrDone, stopped, true)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-stopChan:
		e.stopCommand(commandID)
		// Signal streamOutput goroutines to stop
		close(stopped)
		// Wait for goroutines to finish
		<-stdoutDone
		<-stderrDone
		outputChan <- Output{
			Output:     "\n*** Stopped ***",
			IsComplete: true,
			IsStopped:  true,
		}
		return
	case err := <-done:
		<-stdoutDone
		<-stderrDone

		if err != nil {
			outputChan <- Output{
				Output:     "Command failed: " + err.Error(),
				IsComplete: true,
				IsError:    true,
			}
		} else {
			outputChan <- Output{
				IsComplete: true,
			}
		}
		return
	}
}

func (e *Executor) streamOutput(pipe interface{ Read([]byte) (int, error) }, outputChan chan<- Output, done chan<- bool, stopped <-chan bool, isStderr bool) {
	defer func() { done <- true }()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		select {
		case <-stopped:
			// Stop signal received, exit gracefully
			return
		default:
			line := convertToUTF8(scanner.Text())
			// Use select to avoid panic on closed channel
			select {
			case <-stopped:
				return
			case outputChan <- Output{
				Output:     line,
				IsError:    isStderr,
				IsComplete: false,
			}:
			}
		}
	}
}

func (e *Executor) createCommand(fullCommand string) *exec.Cmd {
	for _, op := range shellOperators {
		if strings.Contains(fullCommand, op) {
			return exec.Command("/bin/bash", "-c", fullCommand)
		}
	}

	parts := strings.Fields(fullCommand)
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], parts[1:]...)
}

func (e *Executor) Stop(commandID string) bool {
	e.stopLock.RLock()
	stopChan, exists := e.stopSignals[commandID]
	e.stopLock.RUnlock()

	if !exists {
		return false
	}

	select {
	case stopChan <- true:
		return true
	default:
		return false
	}
}

func (e *Executor) storeCommand(commandID, fullCommand string, stopChan chan bool) {
	e.commandsLock.Lock()
	e.activeCommands[commandID] = &ActiveCommand{
		FullCommand: fullCommand,
	}
	e.commandsLock.Unlock()

	e.stopLock.Lock()
	e.stopSignals[commandID] = stopChan
	e.stopLock.Unlock()
}

func (e *Executor) removeCommand(commandID string) {
	e.commandsLock.Lock()
	delete(e.activeCommands, commandID)
	e.commandsLock.Unlock()

	e.stopLock.Lock()
	delete(e.stopSignals, commandID)
	e.stopLock.Unlock()
}

func (e *Executor) stopCommand(commandID string) {
	e.commandsLock.RLock()
	activeCmd, exists := e.activeCommands[commandID]
	e.commandsLock.RUnlock()

	if !exists || activeCmd.Cmd == nil || activeCmd.Cmd.Process == nil {
		return
	}

	activeCmd.Cmd.Process.Kill()
}

func generateCommandID(command, target, sessionID string) string {
	if target != "" {
		return fmt.Sprintf("%s-%s-%s", command, target, sessionID)
	}
	return fmt.Sprintf("%s-%s", command, sessionID)
}

// convertToUTF8 converts the input string from GBK to UTF-8 if running on Windows
func convertToUTF8(input string) string {
	if runtime.GOOS != "windows" {
		return input
	}

	// Try to convert from GBK to UTF-8
	decoder := simplifiedchinese.GBK.NewDecoder()
	reader := transform.NewReader(strings.NewReader(input), decoder)
	output, err := io.ReadAll(reader)
	if err != nil {
		// If conversion fails, return original string
		return input
	}
	return string(output)
}
