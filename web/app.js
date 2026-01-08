class YALSClient {
    constructor() {
        this.ws = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.reconnectDelay = 3000;
        this.selectedCommand = null;
        this.commands = [];
        this.currentOutput = '';
        this.isRunning = false;

        this.initElements();
        this.initWebSocket();
        this.bindEvents();
    }

    initElements() {
        this.statusDot = document.getElementById('statusDot');
        this.statusText = document.getElementById('statusText');
        this.hostInfo = document.getElementById('hostInfo');
        this.commandSelector = document.getElementById('commandSelector');
        this.targetInput = document.getElementById('targetInput');
        this.ipVersionSelect = document.getElementById('ipVersionSelect');
        this.executeBtn = document.getElementById('executeBtn');
        this.stopBtn = document.getElementById('stopBtn');
        this.terminalBody = document.getElementById('terminalBody');
        this.rateLimitInfo = document.getElementById('rateLimitInfo');
        this.footerVersion = document.getElementById('footerVersion');
        this.currentSessionID = null;
    }

    async initWebSocket() {
        // Only get session ID if we don't have one yet
        if (!this.currentSessionID) {
            try {
                const response = await fetch('/api/session');
                const data = await response.json();
                this.currentSessionID = data.session_id;
            } catch (error) {
                console.error('Failed to get session ID:', error);
                // If we can't get session ID, schedule reconnect
                this.scheduleReconnect();
                return;
            }
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/${this.currentSessionID}`;

        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            this.setConnectionStatus(true);
            this.reconnectAttempts = 0;
            this.sendMessage({ type: 'get_commands' });
            this.sendMessage({ type: 'get_config' });
        };

        this.ws.onclose = () => {
            this.setConnectionStatus(false);
            this.scheduleReconnect();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.setConnectionStatus(false);
        };

        this.ws.onmessage = (event) => {
            this.handleMessage(JSON.parse(event.data));
        };
    }

    setConnectionStatus(connected) {
        this.statusDot.classList.toggle('connected', connected);
        this.statusText.textContent = connected ? 'Connected' : 'Disconnected';
    }

    scheduleReconnect() {
        if (this.reconnectAttempts < this.maxReconnectAttempts) {
            this.reconnectAttempts++;
            this.statusText.textContent = `Reconnecting (${this.reconnectAttempts}/${this.maxReconnectAttempts})...`;
            setTimeout(() => {
                // Try to reconnect with existing session ID first
                this.initWebSocket();
            }, this.reconnectDelay);
        } else {
            this.statusText.textContent = 'Connection failed. Please refresh the page.';
            this.currentSessionID = null; // Reset session ID for next attempt
        }
    }

    sendMessage(message) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(message));
        }
    }

    handleMessage(data) {
        switch (data.type) {
            case 'app_config':
                this.renderHostInfo(data.host);
                const commands = Array.isArray(data.commands) ? data.commands : [];
                this.commands = commands;
                this.renderCommands(commands);
                if (data.version) {
                    this.footerVersion.textContent = 'Version ' + data.version;
                }
                break;
            case 'command_output':
                this.handleCommandOutput(data);
                break;
        }
    }

    renderHostInfo(hostData) {
        if (!hostData) {
            this.hostInfo.innerHTML = '<div class="empty-state">No host information available</div>';
            return;
        }

        this.hostInfo.innerHTML = `
            <div class="info-item">
                <span class="info-label">Name</span>
                <span class="info-value">${this.escapeHtml(hostData.name || 'N/A')}</span>
            </div>
            <div class="info-item">
                <span class="info-label">Location</span>
                <span class="info-value">${this.escapeHtml(hostData.location || 'N/A')}</span>
            </div>
            <div class="info-item">
                <span class="info-label">Datacenter</span>
                <span class="info-value">${this.escapeHtml(hostData.datacenter || 'N/A')}</span>
            </div>
            <div class="info-item">
                <span class="info-label">Test IP</span>
                <span class="info-value">${this.escapeHtml(hostData.test_ip || 'N/A')}</span>
            </div>
            <div class="info-item">
                <span class="info-label">Description</span>
                <span class="info-value">${this.escapeHtml(hostData.description || 'N/A')}</span>
            </div>
        `;
    }

    renderCommands(commands) {
        if (!commands || commands.length === 0) {
            this.commandSelector.innerHTML = '<div class="empty-state">No commands available</div>';
            return;
        }

        this.commandSelector.innerHTML = commands.map(cmd => `
            <button class="command-btn" data-command="${this.escapeHtml(cmd.name)}">
                <span class="command-btn-name">${this.escapeHtml(cmd.name)}</span>
                <span class="command-btn-desc">${this.escapeHtml(cmd.description || '')}</span>
            </button>
        `).join('');

        if (commands.length > 0) {
            this.selectCommand(commands[0].name);
        }

        this.updateExecuteButton();
    }

    bindEvents() {
        this.commandSelector.addEventListener('click', (e) => {
            if (e.target.classList.contains('command-btn') || e.target.closest('.command-btn')) {
                const btn = e.target.classList.contains('command-btn') ? e.target : e.target.closest('.command-btn');
                this.selectCommand(btn.dataset.command);
            }
        });

        this.targetInput.addEventListener('input', () => this.updateExecuteButton());

        // Add Enter key support for target input
        this.targetInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter' && !this.executeBtn.disabled) {
                this.executeCommand();
            }
        });

        this.executeBtn.addEventListener('click', () => this.executeCommand());
        this.stopBtn.addEventListener('click', () => this.stopCommand());
    }

    selectCommand(commandName) {
        this.selectedCommand = commandName;

        document.querySelectorAll('.command-btn').forEach(btn => {
            btn.classList.toggle('selected', btn.dataset.command === commandName);
        });

        this.updateExecuteButton();
    }

    updateExecuteButton() {
        const hasCommand = this.selectedCommand !== null;
        const targetValue = this.targetInput.value.trim();
        const hasTarget = targetValue !== '';
        const cmd = this.commands.find(c => c.name === this.selectedCommand);
        const ignoreTarget = cmd?.ignore_target || false;

        this.executeBtn.disabled = !hasCommand || (!hasTarget && !ignoreTarget) || this.isRunning;

        if (cmd) {
            if (cmd.ignore_target) {
                this.targetInput.placeholder = 'Not required for this command';
                this.targetInput.disabled = true;
                this.targetInput.value = '';
            } else {
                this.targetInput.placeholder = 'Enter IP address or domain name';
                this.targetInput.disabled = false;
            }
        }
    }

    executeCommand() {
        if (!this.selectedCommand) return;

        this.clearTerminal();

        const target = this.targetInput.value.trim();
        const cmd = this.commands.find(c => c.name === this.selectedCommand);

        if (!cmd.ignore_target && !target) {
            this.appendOutput('Error: Target is required for this command\n', 'error');
            return;
        }

        this.isRunning = true;
        this.executeBtn.disabled = true;
        this.stopBtn.disabled = true; // Keep disabled until we get command_id
        this.targetInput.disabled = true; // Disable target input during execution
        this.ipVersionSelect.disabled = true; // Disable IP version select during execution
        this.disableCommandButtons(); // Disable command selection during execution
        this.currentCommandId = null; // Reset command ID
        this.rateLimitInfo.style.display = 'none';

        this.appendOutput(`<span class="command-line">$ ${this.selectedCommand}${target ? ' ' + target : ''}</span>\n`, 'normal');

        this.sendMessage({
            type: 'execute_command',
            command: this.selectedCommand,
            target: target || '',
            ip_version: this.ipVersionSelect.value
        });
    }

    stopCommand() {
        if (!this.currentCommandId) {
            console.warn('Cannot stop command: command_id not available yet');
            return;
        }

        this.sendMessage({
            type: 'stop_command',
            command_id: this.currentCommandId
        });

        // Disable stop button immediately after sending stop signal
        this.stopBtn.disabled = true;
    }

    handleCommandOutput(data) {
        // Set command ID and enable stop button as soon as we receive it
        if (data.command_id && !this.currentCommandId) {
            this.currentCommandId = data.command_id;
            this.stopBtn.disabled = false; // Enable stop button once we have command_id
        }

        if (data.is_complete) {
            this.isRunning = false;
            this.executeBtn.disabled = false;
            this.stopBtn.disabled = true;
            this.targetInput.disabled = false; // Re-enable target input
            this.ipVersionSelect.disabled = false; // Re-enable IP version select
            this.enableCommandButtons(); // Re-enable command selection
            this.currentCommandId = null;
        }

        if (data.error) {
            this.appendOutput(`Error: ${this.escapeHtml(data.error)}\n`, 'error');
        } else if (data.output) {
            this.appendOutput(this.escapeHtml(data.output), 'normal');
        }
    }

    appendOutput(text, type) {
        if (this.terminalBody.querySelector('.empty-state')) {
            this.terminalBody.innerHTML = '';
        }

        const outputDiv = document.createElement('div');
        outputDiv.className = 'terminal-output ' + type;
        outputDiv.innerHTML = text;
        this.terminalBody.appendChild(outputDiv);

        this.terminalBody.scrollTop = this.terminalBody.scrollHeight;
    }

    clearTerminal() {
        this.terminalBody.innerHTML = '';
    }

    disableCommandButtons() {
        document.querySelectorAll('.command-btn').forEach(btn => {
            btn.disabled = true;
            btn.style.opacity = '0.5';
            btn.style.cursor = 'not-allowed';
        });
    }

    enableCommandButtons() {
        document.querySelectorAll('.command-btn').forEach(btn => {
            btn.disabled = false;
            btn.style.opacity = '';
            btn.style.cursor = 'pointer';
        });
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

document.addEventListener('DOMContentLoaded', () => {
    new YALSClient();
});
