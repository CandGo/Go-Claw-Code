package tools

import (
	"crypto/tls"
	"fmt"
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
		Description: "Fetch a URL and return its content as text.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string", "description": "The URL to fetch"},
				"prompt": map[string]interface{}{"type": "string", "description": "What to extract from the page"},
			},
			"required": []string{"url", "prompt"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			rawURL, _ := input["url"].(string)
			prompt, _ := input["prompt"].(string)

			parsed, err := url.Parse(rawURL)
			if err != nil {
				return "", fmt.Errorf("invalid URL: %w", err)
			}
			// Upgrade to HTTPS for non-localhost
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

			content := stripHTMLTags(string(body))
			if len(content) > 20000 {
				content = content[:20000] + "\n... (truncated)"
			}

			if prompt != "" {
				return fmt.Sprintf("Fetched %s\nPrompt: %s\n\nContent:\n%s", parsed.String(), prompt, content), nil
			}
			return content, nil
		},
	}
}

func webSearchTool() *ToolSpec {
	return &ToolSpec{
		Name:        "WebSearch",
		Description: "Search the web using DuckDuckGo.",
		InputSchema: map[string]interface{}{
			"type": "object",
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
			resp, err := client.Get(searchURL)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
			if err != nil {
				return "", fmt.Errorf("read failed: %w", err)
			}

			results := parseDDGResults(string(body))

			// Filter domains
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
			if blocked, ok := input["blocked_domains"].([]interface{}); ok && len(blocked) > 0 {
				domainSet := make(map[string]bool)
				for _, d := range blocked {
					domainSet[fmt.Sprintf("%v", d)] = true
				}
				var filtered []ddgResult
				for _, r := range results {
					u, err := url.Parse(r.URL)
					if err == nil || !domainSet[u.Host] {
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

	titleRe := regexp.MustCompile(`class="result__a"[^>]*>(.*?)</a>`)
	snippetRe := regexp.MustCompile(`class="result__snippet"[^>]*>(.*?)</a>`)
	urlRe := regexp.MustCompile(`href="(//duckduckgo.com/l/\?uddg=[^"]*)"`)

	titles := titleRe.FindAllStringSubmatch(html, -1)
	snippets := snippetRe.FindAllStringSubmatch(html, -1)
	urls := urlRe.FindAllStringSubmatch(html, -1)

	n := len(titles)
	if len(snippets) < n {
		n = len(snippets)
	}
	if len(urls) < n {
		n = len(urls)
	}

	for i := 0; i < n && i < 20; i++ {
		title := stripHTMLTags(titles[i][1])
		snippet := stripHTMLTags(snippets[i][1])
		rawURL := urls[i][1]

		// Decode DuckDuckGo redirect URL
		if strings.HasPrefix(rawURL, "//duckduckgo.com/l/?uddg=") {
			encoded := strings.TrimPrefix(rawURL, "//duckduckgo.com/l/?uddg=")
			if idx := strings.Index(encoded, "&"); idx != -1 {
				encoded = encoded[:idx]
			}
			decoded, err := url.QueryUnescape(encoded)
			if err == nil {
				rawURL = decoded
			} else {
				rawURL = "https:" + rawURL
			}
		} else {
			rawURL = "https:" + rawURL
		}

		results = append(results, ddgResult{
			Title:   strings.TrimSpace(title),
			URL:     rawURL,
			Snippet: strings.TrimSpace(snippet),
		})
	}
	return results
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTMLTags(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	// Collapse whitespace
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
