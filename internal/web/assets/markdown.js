function initMarkdown() {
    const renderer = new marked.Renderer();

    renderer.code = function(code, language) {
        const lang = language || '';
        let highlighted;
        try {
            if (lang && hljs.getLanguage(lang)) {
                highlighted = hljs.highlight(code, { language: lang }).value;
            } else {
                highlighted = hljs.highlightAuto(code).value;
            }
        } catch {
            highlighted = escapeHtml(code);
        }
        const id = 'code-' + Math.random().toString(36).substr(2, 8);
        return '<div class="code-block"><div class="code-header"><span>' + lang + '</span>' +
            '<button onclick="copyCode(\'' + id + '\')">Copy</button></div>' +
            '<pre><code id="' + id + '" class="hljs">' + highlighted + '</code></pre></div>';
    };

    marked.setOptions({
        renderer: renderer,
        breaks: true,
        gfm: true
    });
}

function renderMarkdown(text) {
    try {
        const html = marked.parse(text);
        return DOMPurify.sanitize(html);
    } catch {
        return DOMPurify.sanitize(escapeHtml(text));
    }
}

// renderMarkdownSafe: for streaming — patches incomplete markdown before rendering
function renderMarkdownSafe(text) {
    if (!text) return '';
    // Patch unclosed inline formatting
    let safe = text;
    // Balance backtick pairs (inline code)
    const backtickCount = (safe.match(/`/g) || []).length;
    if (backtickCount % 2 !== 0) safe += '`';
    // Balance ** pairs (bold)
    const boldCount = (safe.match(/\*\*/g) || []).length;
    if (boldCount % 2 !== 0) safe += '**';
    // Balance * pairs (italic, but not inside **)
    // Balance ~~ pairs (strikethrough)
    const strikeCount = (safe.match(/~~/g) || []).length;
    if (strikeCount % 2 !== 0) safe += '~~';
    // Balance [ pairs (links)
    const bracketCount = (safe.match(/\[/g) || []).length;
    const parenCount = (safe.match(/\]\(/g) || []).length;
    if (bracketCount > parenCount) safe += '](...)';

    try {
        const html = marked.parse(safe);
        return DOMPurify.sanitize(html);
    } catch {
        return DOMPurify.sanitize(escapeHtml(text));
    }
}

function escapeHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function copyCode(id) {
    const el = document.getElementById(id);
    if (el) {
        navigator.clipboard.writeText(el.textContent);
        const btn = el.closest('.code-block').querySelector('button');
        btn.textContent = 'Copied!';
        setTimeout(() => btn.textContent = 'Copy', 1500);
    }
}
