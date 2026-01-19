class YALSClient {
    constructor() {
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 5;
        this.reconnectDelay = 3000;
        this.selectedCommand = null;
        this.commands = [];
        this.isRunning = false;
        this.eventSource = null;
        this.abortController = null;

        this.initElements();
        this.initSession();
        this.bindEvents();
    }

    initElements() {
        this.hostInfo = document.getElementById('hostInfo');
        this.commandSelector = document.getElementById('commandSelect');
        this.targetInput = document.getElementById('targetInput');
        this.ipVersionSelect = document.getElementById('ipVersionSelect');
        this.executeBtn = document.getElementById('executeBtn');
        this.stopBtn = document.getElementById('stopBtn');
        this.terminalBody = document.getElementById('terminalBody');
        this.rateLimitInfo = document.getElementById('rateLimitInfo');
        this.headerVersion = document.getElementById('headerVersion');
        this.currentSessionID = null;
    }

    async initSession() {
        try {
            const response = await fetch('/api/session');
            const data = await response.json();
            this.currentSessionID = data.session_id;
            this.loadNodeData();
        } catch (error) {
            console.error('Failed to get session ID:', error);
            this.scheduleReconnect();
        }
    }

    async loadNodeData() {
        try {
            const response = await fetch(`/api/node?session_id=${encodeURIComponent(this.currentSessionID)}`);
            if (!response.ok) {
                throw new Error('Failed to load node data');
            }
            const data = await response.json();
            this.handleNodeData(data);
            this.setConnectionStatus(true);
            this.reconnectAttempts = 0;
        } catch (error) {
            console.error('Failed to load node data:', error);
            this.setConnectionStatus(false);
            this.scheduleReconnect();
        }
    }

    handleNodeData(data) {
        switch (data.type) {
            case 'app_config':
                this.renderHostInfo(data.host);
                const commands = Array.isArray(data.commands) ? data.commands : [];
                this.commands = commands;
                this.renderCommands(commands);
                if (data.version) {
                    this.headerVersion.textContent = 'Version ' + data.version;
                }
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
            this.commandSelector.innerHTML = '<option value="">No commands available</option>';
            return;
        }

        this.commandSelector.innerHTML = commands.map(cmd => `
            <option value="${this.escapeHtml(cmd.name)}">${this.escapeHtml(cmd.name)}</option>
        `).join('');

        if (commands.length > 0) {
            this.selectCommand(commands[0].name);
        }

        this.updateExecuteButton();
    }

    bindEvents() {
        this.commandSelector.addEventListener('change', (e) => {
            this.selectCommand(e.target.value);
        });

        this.targetInput.addEventListener('input', () => this.updateExecuteButton());

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

        this.commandSelector.value = commandName;

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

    async executeCommand() {
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
        this.stopBtn.disabled = true;
        this.targetInput.disabled = true;
        this.ipVersionSelect.disabled = true;
        this.disableCommandButtons();
        this.currentCommandId = null;
        this.rateLimitInfo.style.display = 'none';

        this.appendOutput(`<span class="command-line">$ ${this.selectedCommand}${target ? ' ' + target : ''}</span>\n`, 'normal');

        try {
            this.abortController = new AbortController();

            const response = await fetch(`/api/exec?session_id=${encodeURIComponent(this.currentSessionID)}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    agent: 'localhost',
                    command: this.selectedCommand,
                    target: target || '',
                    ip_version: this.ipVersionSelect.value
                }),
                signal: this.abortController.signal
            });

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.error);
            }

            this.stopBtn.disabled = false;

            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n\n');
                buffer = lines.pop();

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        try {
                            const data = JSON.parse(line.slice(6));
                            this.handleSSEMessage(data);
                        } catch (e) {
                            console.error('Failed to parse SSE message:', e);
                        }
                    }
                }
            }

        } catch (error) {
            if (error.name !== 'AbortError') {
                console.error('Execute command error:', error);
                this.appendOutput(`Error: ${this.escapeHtml(error.message)}\n`, 'error');
            }
        } finally {
            this.isRunning = false;
            this.executeBtn.disabled = false;
            this.stopBtn.disabled = true;
            this.targetInput.disabled = false;
            this.ipVersionSelect.disabled = false;
            this.enableCommandButtons();
            this.currentCommandId = null;
            this.abortController = null;
        }
    }

    handleSSEMessage(data) {
        if (data.command_id && !this.currentCommandId) {
            this.currentCommandId = data.command_id;
            this.stopBtn.disabled = false;
        }

        if (data.type === 'complete') {
            this.isRunning = false;
            this.executeBtn.disabled = false;
            this.stopBtn.disabled = true;
            this.targetInput.disabled = false;
            this.ipVersionSelect.disabled = false;
            this.enableCommandButtons();
            this.currentCommandId = null;
        }

        if (data.error) {
            this.appendOutput(`Error: ${this.escapeHtml(data.error)}\n`, 'error');
        } else if (data.output) {
            this.appendOutput(this.escapeHtml(data.output), 'normal');
        }
    }

    async stopCommand() {
        if (!this.currentCommandId) {
            console.warn('Cannot stop command: command_id not available yet');
            return;
        }

        if (this.abortController) {
            this.abortController.abort();
        }

        try {
            const response = await fetch(`/api/stop?session_id=${encodeURIComponent(this.currentSessionID)}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({
                    command_id: this.currentCommandId
                })
            });

            const data = await response.json();

            if (data.success) {
                this.appendOutput('\n*** Stopped ***\n', 'normal');
            } else {
                this.appendOutput(`Error: ${this.escapeHtml(data.error || 'Failed to stop command')}\n`, 'error');
            }
        } catch (error) {
            console.error('Stop command error:', error);
            this.appendOutput(`Error: ${this.escapeHtml(error.message)}\n`, 'error');
        }

        this.stopBtn.disabled = true;
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
        this.commandSelector.disabled = true;
    }

    enableCommandButtons() {
        this.commandSelector.disabled = false;
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
