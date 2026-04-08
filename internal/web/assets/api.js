class ClawAPI {
    constructor(url) {
        this.url = url;
        this.ws = null;
        this.handlers = {};
        this.msgId = 0;
    }

    connect() {
        return new Promise((resolve, reject) => {
            this.ws = new WebSocket(this.url);
            this.ws.onopen = () => resolve();
            this.ws.onerror = (e) => reject(e);
            this.ws.onmessage = (e) => {
                try {
                    const msg = JSON.parse(e.data);
                    this._dispatch(msg);
                } catch (err) {
                    console.error('parse error:', err);
                }
            };
            this.ws.onclose = () => this._dispatch({ type: 'disconnected' });
        });
    }

    on(type, handler) {
        if (!this.handlers[type]) this.handlers[type] = [];
        this.handlers[type].push(handler);
    }

    _dispatch(msg) {
        const handlers = this.handlers[msg.type] || [];
        handlers.forEach(h => h(msg));
        const allHandlers = this.handlers['*'] || [];
        allHandlers.forEach(h => h(msg));
    }

    send(content) {
        const id = 'msg-' + (++this.msgId);
        this._send({ type: 'message', id, content });
        return id;
    }

    respondPermission(promptId, decision) {
        this._send({ type: 'permission_response', prompt_id: promptId, decision });
    }

    cancel() {
        this._send({ type: 'cancel' });
    }

    newSession() {
        this._send({ type: 'session_new' });
    }

    command(cmd, args) {
        this._send({ type: 'command', command: cmd, args: args || '' });
    }

    _send(obj) {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(obj));
        }
    }

    close() {
        if (this.ws) this.ws.close();
    }
}
