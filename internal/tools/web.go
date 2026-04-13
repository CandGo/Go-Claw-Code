package tools

import (
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func webFetchTool() *ToolSpec {
	return &ToolSpec{
		Name:        "WebFetch",
		Description: "Fetch a URL and convert its HTML content to structured markdown preserving headings, links, code blocks, tables, and other formatting.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string", "description": "The URL to fetch"},
				"prompt": map[string]interface{}{"type": "string", "description": "What to extract from the page"},
			},
			"required": []string{"url"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			rawURL, _ := input["url"].(string)
			prompt, _ := input["prompt"].(string)

			parsed, err := url.Parse(rawURL)
			if err != nil {
				return "", fmt.Errorf("invalid URL: %w", err)
			}
			if parsed.Scheme == "http" && parsed.Host != "localhost" && !strings.HasPrefix(parsed.Host, "localhost:") {
				parsed.Scheme = "https"
			}

			client := &http.Client{Timeout: 20 * time.Second}
			resp, err := client.Get(parsed.String())
			if err != nil {
				return "", fmt.Errorf("fetch failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return "", fmt.Errorf("HTTP %d", resp.StatusCode)
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
			if err != nil {
				return "", fmt.Errorf("read failed: %w", err)
			}

			pageHTML := string(body)

			// Extract title
			title := extractTagContent(pageHTML, "title")

			// Try to extract <article> content first, then <main>, then fall back to <body>
			contentHTML := extractTagContent(pageHTML, "article")
			if contentHTML == "" {
				contentHTML = extractTagContent(pageHTML, "main")
			}
			if contentHTML == "" {
				contentHTML = extractTagContent(pageHTML, "body")
			}
			if contentHTML == "" {
				contentHTML = pageHTML
			}

			mdContent := htmlToMarkdown(contentHTML)
			if len(mdContent) > 50000 {
				mdContent = mdContent[:50000] + "\n\n... (content truncated at 50000 characters)"
			}

			var result strings.Builder
			result.WriteString("<webfetch_result>\n")
			fmt.Fprintf(&result, "<url>%s</url>\n", parsed.String())
			fmt.Fprintf(&result, "<title>%s</title>\n", escapeXML(title))
			fmt.Fprintf(&result, "<content>\n%s\n</content>\n", mdContent)
			result.WriteString("</webfetch_result>")

			if prompt != "" {
				return fmt.Sprintf("Fetched %s\nPrompt: %s\n\n%s", parsed.String(), prompt, result.String()), nil
			}
			return result.String(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// HTML-to-Markdown converter
// ---------------------------------------------------------------------------

// Precompiled regexes for HTML-to-markdown conversion.
var (
	reComment    = regexp.MustCompile(`<!--[\s\S]*?-->`)
	reHead       = regexp.MustCompile(`(?i)<h([1-6])[^>]*>([\s\S]*?)</h[1-6]>`)
	reLink       = regexp.MustCompile(`(?i)<a\s[^>]*?href="([^"]*)"[^>]*>([\s\S]*?)</a>`)
	reBold       = regexp.MustCompile(`(?i)<(?:strong|b)[^>]*>([\s\S]*?)</(?:strong|b)>`)
	reItalic     = regexp.MustCompile(`(?i)<(?:em|i)[^>]*>([\s\S]*?)</(?:em|i)>`)
	reInlineCode = regexp.MustCompile(`(?i)<code[^>]*>([\s\S]*?)</code>`)
	reBlockquote = regexp.MustCompile(`(?i)<blockquote[^>]*>([\s\S]*?)</blockquote>`)
	reImgAlt     = regexp.MustCompile(`(?i)<img\s[^>]*?alt="([^"]*)"[^>]*?src="([^"]*)"[^>]*>`)
	reImgSrc     = regexp.MustCompile(`(?i)<img\s[^>]*?src="([^"]*)"[^>]*?(?:alt="([^"]*)")?[^>]*>`)
	reHR         = regexp.MustCompile(`(?i)<hr\s*/?\s*>`)
	reBR         = regexp.MustCompile(`(?i)<br\s*/?\s*>`)
	rePre        = regexp.MustCompile(`(?is)<pre[^>]*>([\s\S]*?)</pre>`)
	reParagraph  = regexp.MustCompile(`(?is)<p[^>]*>([\s\S]*?)</p>`)
	reDiv        = regexp.MustCompile(`(?is)<div[^>]*>([\s\S]*?)</div>`)
	reLI         = regexp.MustCompile(`(?is)<li[^>]*>([\s\S]*?)</li>`)
	reUL         = regexp.MustCompile(`(?is)<ul[^>]*>([\s\S]*?)</ul>`)
	reOL         = regexp.MustCompile(`(?is)<ol[^>]*>([\s\S]*?)</ol>`)
	reTable      = regexp.MustCompile(`(?is)<table[^>]*>([\s\S]*?)</table>`)
	reTHead      = regexp.MustCompile(`(?is)<thead[^>]*>([\s\S]*?)</thead>`)
	reTBody      = regexp.MustCompile(`(?is)<tbody[^>]*>([\s\S]*?)</tbody>`)
	reTR         = regexp.MustCompile(`(?is)<tr[^>]*>([\s\S]*?)</tr>`)
	reTH         = regexp.MustCompile(`(?is)<th[^>]*>([\s\S]*?)</th>`)
	reTD         = regexp.MustCompile(`(?is)<td[^>]*>([\s\S]*?)</td>`)
	reStrip      = regexp.MustCompile(`(?is)<(?:script|style|nav|header|footer|noscript|svg|form|iframe)[^>]*>[\s\S]*?</(?:script|style|nav|header|footer|noscript|svg|form|iframe)>`)
	reFigure     = regexp.MustCompile(`(?is)<figure[^>]*>([\s\S]*?)</figure>`)
	reFigCap     = regexp.MustCompile(`(?is)<figcaption[^>]*>([\s\S]*?)</figcaption>`)
	reDL         = regexp.MustCompile(`(?is)<dl[^>]*>([\s\S]*?)</dl>`)
	reDT         = regexp.MustCompile(`(?is)<dt[^>]*>([\s\S]*?)</dt>`)
	reDD         = regexp.MustCompile(`(?is)<dd[^>]*>([\s\S]*?)</dd>`)
	reTag        = regexp.MustCompile(`<[^>]+>`)
)

// extractTagContent extracts the inner content of the first occurrence of a given HTML tag.
func extractTagContent(s, tag string) string {
	pat := regexp.MustCompile(`(?is)<` + tag + `[^>]*>([\s\S]*?)</` + tag + `>`)
	m := pat.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// escapeXML escapes special characters for safe embedding in XML-like output.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// decodeHTMLEntities decodes HTML entities to their character equivalents.
func decodeHTMLEntities(s string) string {
	s = html.UnescapeString(s)
	// Replace non-breaking spaces with regular spaces
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return s
}

// htmlToMarkdown converts HTML to structured markdown text.
func htmlToMarkdown(h string) string {
	s := h

	// 1. Remove HTML comments
	s = reComment.ReplaceAllString(s, "")

	// 2. Strip unwanted tags entirely (with their content)
	s = reStrip.ReplaceAllString(s, "")

	// 3. Process tables before other inline elements
	s = convertTables(s)

	// 4. Process block-level elements
	// Preformatted code blocks (before other conversions)
	s = rePre.ReplaceAllStringFunc(s, func(m string) string {
		sub := rePre.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		code := sub[1]
		// Remove inner <code> tags if present
		code = reInlineCode.ReplaceAllString(code, "$1")
		code = decodeHTMLEntities(code)
		code = strings.TrimSpace(code)
		return "\n```\n" + code + "\n```\n"
	})

	// Headings
	s = reHead.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHead.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		level := len(sub[1])
		text := cleanInline(sub[2])
		return fmt.Sprintf("\n%s %s\n", strings.Repeat("#", level), text)
	})

	// Blockquotes
	s = reBlockquote.ReplaceAllStringFunc(s, func(m string) string {
		sub := reBlockquote.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := htmlToMarkdown(sub[1]) // recurse for nested content
		lines := strings.Split(inner, "\n")
		var b strings.Builder
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				b.WriteString(">\n")
			} else {
				b.WriteString("> " + trimmed + "\n")
			}
		}
		return "\n" + b.String() + "\n"
	})

	// Figures
	s = reFigure.ReplaceAllStringFunc(s, func(m string) string {
		sub := reFigure.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := sub[1]
		// Extract figcaption
		cap := reFigCap.FindStringSubmatch(inner)
		captionText := ""
		if len(cap) > 1 {
			captionText = cleanInline(cap[1])
		}
		// Convert images and recurse on remaining content
		inner = convertImages(inner)
		inner = htmlToMarkdown(inner)
		if captionText != "" {
			inner += "\n*" + captionText + "*"
		}
		return "\n" + inner + "\n"
	})

	// Definition lists
	s = reDL.ReplaceAllStringFunc(s, func(m string) string {
		sub := reDL.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := sub[1]
		inner = reDT.ReplaceAllStringFunc(inner, func(dt string) string {
			dtSub := reDT.FindStringSubmatch(dt)
			if len(dtSub) > 1 {
				return "\n**" + cleanInline(dtSub[1]) + "**\n"
			}
			return dt
		})
		inner = reDD.ReplaceAllStringFunc(inner, func(dd string) string {
			ddSub := reDD.FindStringSubmatch(dd)
			if len(ddSub) > 1 {
				return ": " + cleanInline(ddSub[1]) + "\n"
			}
			return dd
		})
		return inner
	})

	// Ordered lists
	s = reOL.ReplaceAllStringFunc(s, func(m string) string {
		sub := reOL.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return convertListItems(sub[1], true)
	})

	// Unordered lists
	s = reUL.ReplaceAllStringFunc(s, func(m string) string {
		sub := reUL.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		return convertListItems(sub[1], false)
	})

	// Images
	s = convertImages(s)

	// Links
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		href := sub[1]
		text := cleanInline(sub[2])
		if text == "" {
			text = href
		}
		return "[" + text + "](" + href + ")"
	})

	// Bold
	s = reBold.ReplaceAllString(s, "**$1**")

	// Italic
	s = reItalic.ReplaceAllString(s, "*$1*")

	// Inline code
	s = reInlineCode.ReplaceAllString(s, "`$1`")

	// Paragraphs
	s = reParagraph.ReplaceAllString(s, "\n\n$1\n\n")

	// Divs
	s = reDiv.ReplaceAllString(s, "\n$1\n")

	// HR
	s = reHR.ReplaceAllString(s, "\n---\n")

	// BR
	s = reBR.ReplaceAllString(s, "\n")

	// 5. Strip all remaining HTML tags
	s = reTag.ReplaceAllString(s, "")

	// 6. Decode HTML entities
	s = decodeHTMLEntities(s)

	// 7. Clean up excessive whitespace
	s = collapseBlankLines(s)

	return strings.TrimSpace(s)
}

// convertImages converts <img> tags to markdown image syntax.
func convertImages(s string) string {
	// Pattern with alt before src
	s = reImgAlt.ReplaceAllString(s, "![$1]($2)")
	// Pattern with src before alt (or no alt)
	s = reImgSrc.ReplaceAllStringFunc(s, func(m string) string {
		sub := reImgSrc.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		src := sub[1]
		alt := ""
		if len(sub) > 2 {
			alt = sub[2]
		}
		return "![" + alt + "](" + src + ")"
	})
	return s
}

// convertListItems processes <li> items within a list and returns markdown.
func convertListItems(listContent string, ordered bool) string {
	items := reLI.FindAllStringSubmatch(listContent, -1)
	var b strings.Builder
	for i, item := range items {
		if len(item) < 2 {
			continue
		}
		text := cleanInline(item[1])
		if ordered {
			fmt.Fprintf(&b, "%d. %s\n", i+1, text)
		} else {
			b.WriteString("- " + text + "\n")
		}
	}
	return "\n" + b.String() + "\n"
}

// convertTables converts HTML tables to markdown tables.
func convertTables(s string) string {
	return reTable.ReplaceAllStringFunc(s, func(m string) string {
		inner := m

		var headerRows, bodyRows []string

		// Process thead
		theadMatch := reTHead.FindStringSubmatch(inner)
		if len(theadMatch) > 1 {
			headerRows = extractTRs(theadMatch[1])
		}

		// Process tbody
		tbodyMatch := reTBody.FindStringSubmatch(inner)
		if len(tbodyMatch) > 1 {
			bodyRows = extractTRs(tbodyMatch[1])
		}

		// If no thead/tbody, treat all rows as body rows
		if len(headerRows) == 0 && len(bodyRows) == 0 {
			bodyRows = extractTRs(inner)
		}

		var b strings.Builder
		b.WriteString("\n")

		if len(headerRows) > 0 {
			cells := extractCells(headerRows[0])
			b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
			b.WriteString("| " + strings.Repeat("--- | ", len(cells)))
			b.WriteString("\n")
			for _, row := range headerRows[1:] {
				cells := extractCells(row)
				b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
			}
		} else if len(bodyRows) > 0 {
			// Use first body row as header
			cells := extractCells(bodyRows[0])
			b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
			b.WriteString("| " + strings.Repeat("--- | ", len(cells)))
			b.WriteString("\n")
			bodyRows = bodyRows[1:]
		}

		for _, row := range bodyRows {
			cells := extractCells(row)
			b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		}

		b.WriteString("\n")
		return b.String()
	})
}

// extractTRs extracts all table row contents from HTML.
func extractTRs(s string) []string {
	matches := reTR.FindAllStringSubmatch(s, -1)
	var rows []string
	for _, m := range matches {
		if len(m) > 1 {
			rows = append(rows, m[1])
		}
	}
	return rows
}

// extractCells extracts th and td cells from a table row and returns cleaned cell text.
func extractCells(row string) []string {
	var cells []string
	for _, re := range []*regexp.Regexp{reTH, reTD} {
		matches := re.FindAllStringSubmatch(row, -1)
		for _, m := range matches {
			if len(m) > 1 {
				cell := cleanInline(m[1])
				// Escape pipes in cell content
				cell = strings.ReplaceAll(cell, "|", "\\|")
				cells = append(cells, cell)
			}
		}
		if len(cells) > 0 {
			break
		}
	}

	if len(cells) == 0 {
		cells = []string{""}
	}
	return cells
}

// cleanInline processes inline HTML elements within a text fragment and strips
// remaining tags, returning plain text suitable for embedding in markdown.
func cleanInline(s string) string {
	s = convertImages(s)
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		return "[" + cleanInline(sub[2]) + "](" + sub[1] + ")"
	})
	s = reBold.ReplaceAllString(s, "**$1**")
	s = reItalic.ReplaceAllString(s, "*$1*")
	s = reInlineCode.ReplaceAllString(s, "`$1`")
	s = reBR.ReplaceAllString(s, " ")
	// Strip remaining tags
	s = reTag.ReplaceAllString(s, "")
	s = decodeHTMLEntities(s)
	s = strings.TrimSpace(s)
	// Collapse internal whitespace
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// collapseBlankLines reduces runs of 3+ blank lines down to 2.
func collapseBlankLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// ---------------------------------------------------------------------------
// WebSearch tool
// ---------------------------------------------------------------------------

// Pre-compiled DDG parsing regexes (avoids recompilation on every call)
var (
	ddgResultBlockRe = regexp.MustCompile(`(?s)<div class="result[^"]*">\s*.*?</div>\s*</div>`)
	ddgTitleRe       = regexp.MustCompile(`class="result__a"[^>]*>(.*?)</a>`)
	ddgSnippetRe     = regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</(?:a|span)>`)
	ddgURLRe         = regexp.MustCompile(`href="(//duckduckgo\.com/l/\?uddg=[^"]*)"`)
	ddgFallbackURLRe = regexp.MustCompile(`class="result__url"[^>]*>(.*?)</a>`)
)

const ddgUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

func webSearchTool() *ToolSpec {
	return &ToolSpec{
		Name:        "WebSearch",
		Description: "Search the web using DuckDuckGo. Returns up to 10 results with titles, URLs, and snippets.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"query":           map[string]interface{}{"type": "string", "description": "Search query"},
				"allowed_domains": map[string]interface{}{"type": "array", "description": "Only include these domains"},
				"blocked_domains": map[string]interface{}{"type": "array", "description": "Exclude these domains"},
			},
			"required": []string{"query"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			query, _ := input["query"].(string)
			if query == "" {
				return "", fmt.Errorf("query is required")
			}

			searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

			client := &http.Client{
				Timeout: 15 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
				},
			}

			req, err := http.NewRequest("GET", searchURL, nil)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}
			req.Header.Set("User-Agent", ddgUserAgent)

			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return "", fmt.Errorf("search returned HTTP %d", resp.StatusCode)
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
			if err != nil {
				return "", fmt.Errorf("read failed: %w", err)
			}

			results := parseDDGResults(string(body))

			// Filter by allowed domains
			if allowed, ok := input["allowed_domains"].([]interface{}); ok && len(allowed) > 0 {
				domainSet := make(map[string]bool)
				for _, d := range allowed {
					domainSet[fmt.Sprintf("%v", d)] = true
				}
				var filtered []ddgResult
				for _, r := range results {
					u, err := url.Parse(r.URL)
					if err == nil && domainSet[u.Host] {
						filtered = append(filtered, r)
					}
				}
				results = filtered
			}

			// Filter by blocked domains
			if blocked, ok := input["blocked_domains"].([]interface{}); ok && len(blocked) > 0 {
				domainSet := make(map[string]bool)
				for _, d := range blocked {
					domainSet[fmt.Sprintf("%v", d)] = true
				}
				var filtered []ddgResult
				for _, r := range results {
					u, err := url.Parse(r.URL)
					if err != nil {
						continue
					}
					excluded := false
					for domain := range domainSet {
						if u.Host == domain || strings.HasSuffix(u.Host, "."+domain) {
							excluded = true
							break
						}
					}
					if !excluded {
						filtered = append(filtered, r)
					}
				}
				results = filtered
			}

			if len(results) == 0 {
				return "No results found.", nil
			}

			var buf strings.Builder
			for i, r := range results {
				if i >= 10 {
					break
				}
				fmt.Fprintf(&buf, "%d. [%s](%s)\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet)
			}
			return buf.String(), nil
		},
	}
}

type ddgResult struct {
	Title   string
	URL     string
	Snippet string
}

func parseDDGResults(html string) []ddgResult {
	var results []ddgResult

	// Extract each result block and parse individually to avoid index misalignment
	blocks := ddgResultBlockRe.FindAllString(html, -1)
	if len(blocks) == 0 {
		// Fallback to the old index-based approach
		return parseDDGResultsLegacy(html)
	}

	for _, block := range blocks {
		titleMatch := ddgTitleRe.FindStringSubmatch(block)
		snippetMatch := ddgSnippetRe.FindStringSubmatch(block)
		urlMatch := ddgURLRe.FindStringSubmatch(block)

		if len(titleMatch) < 2 || len(urlMatch) < 2 {
			continue
		}

		title := stripHTMLTags(titleMatch[1])
		rawURL := decodeDDGURL(urlMatch[1])
		snippet := ""
		if len(snippetMatch) >= 2 {
			snippet = stripHTMLTags(snippetMatch[1])
		}

		results = append(results, ddgResult{
			Title:   strings.TrimSpace(title),
			URL:     rawURL,
			Snippet: strings.TrimSpace(snippet),
		})

		if len(results) >= 20 {
			break
		}
	}
	return results
}

// parseDDGResultsLegacy is the fallback index-based parser for when block parsing fails.
func parseDDGResultsLegacy(html string) []ddgResult {
	var results []ddgResult

	titles := ddgTitleRe.FindAllStringSubmatch(html, -1)
	snippets := ddgSnippetRe.FindAllStringSubmatch(html, -1)
	urls := ddgURLRe.FindAllStringSubmatch(html, -1)

	n := len(titles)
	if len(urls) < n {
		n = len(urls)
	}

	for i := 0; i < n && i < 20; i++ {
		title := stripHTMLTags(titles[i][1])
		rawURL := decodeDDGURL(urls[i][1])
		snippet := ""
		if i < len(snippets) {
			snippet = stripHTMLTags(snippets[i][1])
		}

		results = append(results, ddgResult{
			Title:   strings.TrimSpace(title),
			URL:     rawURL,
			Snippet: strings.TrimSpace(snippet),
		})
	}
	return results
}

// decodeDDGURL extracts the actual URL from DDG's redirect URL.
func decodeDDGURL(rawURL string) string {
	if strings.HasPrefix(rawURL, "//duckduckgo.com/l/?uddg=") {
		encoded := strings.TrimPrefix(rawURL, "//duckduckgo.com/l/?uddg=")
		if idx := strings.Index(encoded, "&"); idx != -1 {
			encoded = encoded[:idx]
		}
		decoded, err := url.QueryUnescape(encoded)
		if err == nil {
			return decoded
		}
	}
	return "https:" + rawURL
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTags(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
