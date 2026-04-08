(function() {
    const api = new ClawAPI('ws://' + location.host + '/ws');
    const messages = document.getElementById('messages');
    const input = document.getElementById('input');
    const permBar = document.getElementById('permission-bar');
    let streaming = false;
    let currentMsgId = null;
    let currentText = '';
    let currentEl = null;
    let currentPermId = null;

    initMarkdown();

    // --- API event handlers ---

    api.on('connected', (msg) => {
        document.getElementById('model-badge').textContent = msg.model || 'unknown';
        document.getElementById('version-badge').textContent = 'v' + (msg.version || '?');
        addSystemMsg('Connected to Go-Claw-Code');
    });

    api.on('disconnected', () => {
        addSystemMsg('Disconnected');
        setStreaming(false);
    });

    api.on('text_delta', (msg) => {
        currentText += msg.text;
        appendText(msg.text);
    });

    api.on('thinking_delta', (msg) => {
        currentText += msg.text;
        appendText(msg.text);
    });

    api.on('tool_use', (msg) => {
        ensureStreamingEl();
        const card = document.createElement('div');
        card.className = 'tool-card';
        card.id = 'tool-' + msg.tool_id;
        const inputStr = JSON.stringify(msg.tool_input || {}, null, 2);
        const truncated = inputStr.length > 500 ? inputStr.substring(0, 500) + '...' : inputStr;
        card.innerHTML = '<div class="tool-card-header"><span class="spinner"></span> ' +
            escapeHtml(msg.tool_name) + '</div><pre>' + escapeHtml(truncated) + '</pre>';
        currentEl.appendChild(card);
        scrollToBottom();
    });

    api.on('tool_result', (msg) => {
        const card = document.getElementById('tool-' + msg.tool_id);
        if (card) {
            const spinner = card.querySelector('.spinner');
            if (spinner) spinner.remove();
            const result = msg.text || '';
            const truncated = result.length > 5000 ? result.substring(0, 5000) + '\n...(truncated)' : result;
            const cls = msg.is_error ? 'error' : '';
            card.innerHTML += '<pre class="' + cls + '">' + escapeHtml(truncated) + '</pre>';
        }
        scrollToBottom();
    });

    api.on('turn_done', (msg) => {
        // Final markdown render for complete text
        if (currentEl) {
            const textNode = currentEl.querySelector('.msg-raw');
            if (textNode && currentText) {
                textNode.innerHTML = renderMarkdown(currentText);
                textNode.className = 'msg-content';
            }
            currentEl.classList.remove('streaming-cursor');
        }
        currentEl = null;
        currentText = '';
        if (msg.usage) {
            const info = 'Tokens: ' + msg.usage.input_tokens + ' in / ' + msg.usage.output_tokens + ' out';
            addSystemMsg(info);
        }
        setStreaming(false);
    });

    api.on('error', (msg) => {
        addSystemMsg('Error: ' + msg.message);
        if (currentEl) {
            currentEl.classList.remove('streaming-cursor');
        }
        currentEl = null;
        currentText = '';
        setStreaming(false);
    });

    api.on('permission_request', (msg) => {
        currentPermId = msg.prompt_id;
        document.getElementById('perm-tool').textContent = msg.tool_name;
        document.getElementById('perm-input').textContent = msg.text || '';
        permBar.classList.remove('hidden');
    });

    api.on('command_output', (msg) => {
        addSystemMsg(msg.output);
    });

    api.on('session_info', (msg) => {
        addSystemMsg('Session: ' + msg.session_id + ' (' + msg.message_count + ' messages)');
    });

    // --- Streaming text: lightweight append, no markdown ---

    function ensureStreamingEl() {
        if (!currentEl) {
            currentEl = document.createElement('div');
            currentEl.className = 'msg msg-assistant streaming-cursor';
            messages.appendChild(currentEl);
        }
    }

    function appendText(text) {
        ensureStreamingEl();
        let textNode = currentEl.querySelector('.msg-raw');
        if (!textNode) {
            textNode = document.createElement('div');
            textNode.className = 'msg-raw';
            currentEl.appendChild(textNode);
        }
        // Fast: just append escaped text during streaming
        textNode.textContent += text;
        scrollToBottom();
    }

    // --- UI actions ---

    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            send();
        }
    });

    input.addEventListener('input', () => {
        input.style.height = 'auto';
        input.style.height = Math.min(input.scrollHeight, 150) + 'px';
    });

    document.getElementById('btn-send').addEventListener('click', send);
    document.getElementById('btn-cancel').addEventListener('click', () => api.cancel());
    document.getElementById('btn-new').addEventListener('click', () => api.newSession());

    document.getElementById('btn-theme').addEventListener('click', () => {
        const html = document.documentElement;
        const next = html.dataset.theme === 'dark' ? 'light' : 'dark';
        html.dataset.theme = next;
        const link = document.getElementById('hljs-theme');
        link.href = next === 'dark'
            ? 'https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css'
            : 'https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github.min.css';
    });

    document.getElementById('btn-allow').addEventListener('click', () => {
        if (currentPermId) api.respondPermission(currentPermId, 'allow');
        permBar.classList.add('hidden');
    });
    document.getElementById('btn-allow-always').addEventListener('click', () => {
        if (currentPermId) api.respondPermission(currentPermId, 'allow_always');
        permBar.classList.add('hidden');
    });
    document.getElementById('btn-deny').addEventListener('click', () => {
        if (currentPermId) api.respondPermission(currentPermId, 'deny');
        permBar.classList.add('hidden');
    });

    // --- Helpers ---

    function send() {
        const text = input.value.trim();
        if (!text || streaming) return;
        input.value = '';
        input.style.height = 'auto';
        addUserMsg(text);
        currentMsgId = api.send(text);
        currentText = '';
        currentEl = null;
        setStreaming(true);
    }

    function addUserMsg(text) {
        const div = document.createElement('div');
        div.className = 'msg msg-user';
        div.textContent = text;
        messages.appendChild(div);
        scrollToBottom();
    }

    function addSystemMsg(text) {
        const div = document.createElement('div');
        div.className = 'msg msg-assistant';
        div.style.fontSize = '12px';
        div.style.color = 'var(--text2)';
        div.style.background = 'transparent';
        div.style.border = 'none';
        div.textContent = text;
        messages.appendChild(div);
        scrollToBottom();
    }

    function setStreaming(val) {
        streaming = val;
        document.getElementById('btn-send').classList.toggle('hidden', val);
        document.getElementById('btn-cancel').classList.toggle('hidden', !val);
        input.disabled = val;
        if (!val) input.focus();
    }

    function scrollToBottom() {
        requestAnimationFrame(() => {
            messages.scrollTop = messages.scrollHeight;
        });
    }

    // --- Connect ---
    api.connect().catch((e) => {
        addSystemMsg('Failed to connect: ' + e.message);
    });
})();
