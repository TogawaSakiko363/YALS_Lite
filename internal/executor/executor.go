package executor

import (
	"YALS/internal/config"
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"
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
	cmdConfig, exists := e.config.Commands[commandName]
	if !exists {
		outputChan <- Output{
			Error:      "Command not found: " + commandName,
			IsComplete: true,
			IsError:    true,
		}
		return ""
	}

	fullCommand := cmdConfig.Template
	if target != "" && !cmdConfig.IgnoreTarget {
		fullCommand = cmdConfig.Template + " " + target
	}

	commandID := generateCommandID(commandName, target, sessionID)
	stopChan := make(chan bool, 1)

	e.storeCommand(commandID, fullCommand, stopChan)

	go e.runCommand(commandID, fullCommand, cmdConfig.IgnoreTarget, stopChan, outputChan)

	return commandID
}

func (e *Executor) runCommand(commandID, fullCommand string, ignoreTarget bool, stopChan <-chan bool, outputChan chan<- Output) {
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

	go e.streamOutput(stdout, outputChan, stdoutDone, false)
	go e.streamOutput(stderr, outputChan, stderrDone, true)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-stopChan:
		e.stopCommand(commandID)
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

func (e *Executor) streamOutput(pipe interface{ Read([]byte) (int, error) }, outputChan chan<- Output, done chan<- bool, isStderr bool) {
	defer func() { done <- true }()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		outputChan <- Output{
			Output:     line,
			IsError:    isStderr,
			IsComplete: false,
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
