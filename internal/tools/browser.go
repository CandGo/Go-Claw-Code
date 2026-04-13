package tools

import (
	"fmt"
	"strings"

	"github.com/CandGo/Go-Claw-Code/internal/browser"
	"github.com/CandGo/Go-Claw-Code/internal/config"
)

// resolveTargetID gets the target ID from input or auto-creates a new tab.
func resolveTargetID(input map[string]interface{}) (string, error) {
	mgr := browser.DefaultManager
	if mgr == nil {
		return "", fmt.Errorf("browser not initialized (enable browser feature flag)")
	}

	// If target_id provided, use it
	if tid, _ := input["target_id"].(string); tid != "" {
		return tid, nil
	}

	// Auto-create a new tab
	targetID, err := mgr.NewTab("about:blank")
	if err != nil {
		return "", fmt.Errorf("failed to create tab: %w", err)
	}
	return targetID, nil
}

// browserNewTabTool creates a new browser tab.
func browserNewTabTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_new_tab",
		Permission:  PermDangerFullAccess,
		Description: "Create a new browser tab. Returns the target_id for use with other browser tools.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to open (default: about:blank)",
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			mgr := browser.DefaultManager
			if mgr == nil {
				return "", fmt.Errorf("browser not initialized")
			}
			url, _ := input["url"].(string)
			targetID, err := mgr.NewTab(url)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Created tab: %s", targetID), nil
		},
	}
}

// browserCloseTabTool closes a browser tab.
func browserCloseTabTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_close_tab",
		Permission:  PermDangerFullAccess,
		Description: "Close a browser tab. Always close tabs you created when done.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_id": map[string]interface{}{
					"type":        "string",
					"description": "Target ID of the tab to close",
				},
			},
			"required": []string{"target_id"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			mgr := browser.DefaultManager
			if mgr == nil {
				return "", fmt.Errorf("browser not initialized")
			}
			targetID, _ := input["target_id"].(string)
			if err := mgr.CloseTab(targetID); err != nil {
				return "", err
			}
			return fmt.Sprintf("Closed tab: %s", targetID), nil
		},
	}
}

// browserListTabsTool lists all open browser tabs.
func browserListTabsTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_list_tabs",
		Permission:  PermReadOnly,
		Description: "List all open browser tabs with their titles and URLs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			mgr := browser.DefaultManager
			if mgr == nil {
				return "", fmt.Errorf("browser not initialized")
			}
			tabs, err := mgr.ListTabs()
			if err != nil {
				return "", err
			}
			var lines []string
			for _, tab := range tabs {
				id, _ := tab["targetId"].(string)
				title, _ := tab["title"].(string)
				url, _ := tab["url"].(string)
				lines = append(lines, fmt.Sprintf("  %s  %s  %s", id, title, url))
			}
			if len(lines) == 0 {
				return "No tabs open", nil
			}
			return "Open tabs:\n" + strings.Join(lines, "\n"), nil
		},
	}
}

// browserNavigateTool navigates a tab to a URL.
func browserNavigateTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_navigate",
		Permission:  PermDangerFullAccess,
		Description: "Navigate a browser tab to a URL. Auto-creates tab if target_id not provided. Preserves login state.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to navigate to",
				},
				"target_id": map[string]interface{}{
					"type":        "string",
					"description": "Tab target ID (auto-creates new tab if omitted)",
				},
			},
			"required": []string{"url"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			url, _ := input["url"].(string)
			if url == "" {
				return "", fmt.Errorf("url is required")
			}
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			result, err := mgr.Navigate(targetID, url)
			if err != nil {
				return "", err
			}

			// Screenshot
			b64, w, h, ssErr := mgr.Screenshot(targetID)
			if ssErr == nil {
				return fmt.Sprintf("target_id: %s\nNavigated to: %s\nTitle: %s\n[Screenshot %dx%d]\n[img:image/png:%s]",
					targetID, result.URL, result.Title, w, h, b64), nil
			}
			return fmt.Sprintf("target_id: %s\nNavigated to: %s\nTitle: %s",
				targetID, result.URL, result.Title), nil
		},
	}
}

// browserClickTool clicks on an element (JS-level).
func browserClickTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_click",
		Permission:  PermDangerFullAccess,
		Description: "Click on an element via JS el.click(). Fast, covers most scenarios.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector": map[string]interface{}{
					"type":        "string",
					"description": "CSS selector",
				},
				"target_id": map[string]interface{}{
					"type":        "string",
					"description": "Tab target ID (auto-creates tab if omitted)",
				},
			},
			"required": []string{"selector"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			selector, _ := input["selector"].(string)
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			result, err := mgr.Click(targetID, selector)
			if err != nil {
				return "", err
			}

			b64, w, h, ssErr := mgr.Screenshot(targetID)
			if ssErr == nil {
				return fmt.Sprintf("target_id: %s\n%s\n[Screenshot %dx%d]\n[img:image/png:%s]",
					targetID, result, w, h, b64), nil
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserClickAtTool performs a real CDP mouse click.
func browserClickAtTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_click_at",
		Permission:  PermDangerFullAccess,
		Description: "Real CDP mouse click (Input.dispatchMouseEvent). Counts as user gesture — triggers file dialogs, bypasses anti-automation.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector": map[string]interface{}{
					"type":        "string",
					"description": "CSS selector",
				},
				"target_id": map[string]interface{}{
					"type":        "string",
					"description": "Tab target ID",
				},
			},
			"required": []string{"selector"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			selector, _ := input["selector"].(string)
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			result, err := mgr.ClickAt(targetID, selector)
			if err != nil {
				return "", err
			}

			b64, w, h, ssErr := mgr.Screenshot(targetID)
			if ssErr == nil {
				return fmt.Sprintf("target_id: %s\n%s\n[Screenshot %dx%d]\n[img:image/png:%s]",
					targetID, result, w, h, b64), nil
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserTypeTool types text into an element.
func browserTypeTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_type",
		Permission:  PermDangerFullAccess,
		Description: "Type text into an input field. Handles React/Vue controlled inputs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS selector"},
				"text":     map[string]interface{}{"type": "string", "description": "Text to type"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
			"required": []string{"selector", "text"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			selector, _ := input["selector"].(string)
			text, _ := input["text"].(string)
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			result, err := mgr.Type(targetID, selector, text)
			if err != nil {
				return "", err
			}

			b64, w, h, ssErr := mgr.Screenshot(targetID)
			if ssErr == nil {
				return fmt.Sprintf("target_id: %s\n%s\n[Screenshot %dx%d]\n[img:image/png:%s]",
					targetID, result, w, h, b64), nil
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserScreenshotTool takes a screenshot.
func browserScreenshotTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_screenshot",
		Permission:  PermReadOnly,
		Description: "Take a screenshot of a browser tab.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			b64, w, h, err := mgr.Screenshot(targetID)
			if err != nil {
				return "", err
			}

			url, title, _ := mgr.GetPageURL(targetID)
			return fmt.Sprintf("target_id: %s\n[Screenshot of %s (%s) %dx%d]\n[img:image/png:%s]",
				targetID, title, url, w, h, b64), nil
		},
	}
}

// browserGetContentTool returns page text content.
func browserGetContentTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_get_content",
		Permission:  PermReadOnly,
		Description: "Get the visible text content of a browser page, or text of specific elements.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector":  map[string]interface{}{"type": "string", "description": "Optional CSS selector for specific elements"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			url, title, _ := mgr.GetPageURL(targetID)

			selector, _ := input["selector"].(string)
			if selector != "" {
				text, err := mgr.QuerySelectorAll(targetID, selector)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("target_id: %s\nPage: %s (%s)\nElements matching '%s':\n%s",
					targetID, title, url, selector, text), nil
			}

			text, err := mgr.GetTextContent(targetID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("target_id: %s\nPage: %s (%s)\n\n%s", targetID, title, url, text), nil
		},
	}
}

// browserScrollTool scrolls the page.
func browserScrollTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_scroll",
		Permission:  PermDangerFullAccess,
		Description: "Scroll the page. Triggers lazy loading. Use 'bottom' to scroll to end.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"direction": map[string]interface{}{"type": "string", "description": "down, up, top, bottom"},
				"y":         map[string]interface{}{"type": "integer", "description": "Pixels (default 3000)"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			direction, _ := input["direction"].(string)
			if direction == "" {
				direction = "down"
			}
			y := 3000
			if v, ok := input["y"].(float64); ok && v > 0 {
				y = int(v)
			}
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}

			mgr := browser.DefaultManager
			result, err := mgr.Scroll(targetID, direction, y)
			if err != nil {
				return "", err
			}

			b64, w, h, ssErr := mgr.Screenshot(targetID)
			if ssErr == nil {
				return fmt.Sprintf("target_id: %s\n%s\n[Screenshot %dx%d]\n[img:image/png:%s]",
					targetID, result, w, h, b64), nil
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserBackTool navigates back.
func browserBackTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_back",
		Permission:  PermDangerFullAccess,
		Description: "Go back in browser history.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}
			mgr := browser.DefaultManager
			result, err := mgr.Back(targetID)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserEvalTool executes JavaScript.
func browserEvalTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_eval",
		Permission:  PermDangerFullAccess,
		Description: "Execute JavaScript in a browser tab. Use to query DOM, extract data, manipulate elements, call page APIs.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"script":    map[string]interface{}{"type": "string", "description": "JavaScript expression"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
			"required": []string{"script"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			script, _ := input["script"].(string)
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}
			mgr := browser.DefaultManager
			result, err := mgr.Eval(targetID, script)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("target_id: %s\n%s", targetID, result), nil
		},
	}
}

// browserPressKeyTool presses a keyboard key.
func browserPressKeyTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_press_key",
		Permission:  PermDangerFullAccess,
		Description: "Press a keyboard key in the browser.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key":       map[string]interface{}{"type": "string", "description": "Key (Enter, Tab, Escape, etc.)"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
			"required": []string{"key"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			key, _ := input["key"].(string)
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}
			mgr := browser.DefaultManager
			return mgr.PressKey(targetID, key)
		},
	}
}

// browserSetFilesTool sets files on a file input.
func browserSetFilesTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_set_files",
		Permission:  PermDangerFullAccess,
		Description: "Upload files to a file input element. Bypasses file dialog.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"selector":  map[string]interface{}{"type": "string", "description": "CSS selector for input[type=file]"},
				"files":     map[string]interface{}{"type": "array", "description": "Array of file paths"},
				"target_id": map[string]interface{}{"type": "string", "description": "Tab target ID"},
			},
			"required": []string{"selector", "files"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			selector, _ := input["selector"].(string)
			var files []string
			if arr, ok := input["files"].([]interface{}); ok {
				for _, f := range arr {
					if s, ok := f.(string); ok {
						files = append(files, s)
					}
				}
			}
			targetID, err := resolveTargetID(input)
			if err != nil {
				return "", err
			}
			mgr := browser.DefaultManager
			if err := mgr.SetFiles(targetID, selector, files); err != nil {
				return "", err
			}
			return fmt.Sprintf("Set %d files on %s", len(files), selector), nil
		},
	}
}

// browserSiteExpTool reads/writes site experience files.
func browserSiteExpTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_site_experience",
		Permission:  PermReadOnly,
		Description: "Read or write site-specific browsing experience. Read before operating on a known site. Write after discovering patterns.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "read, write, or list",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Domain name (e.g. xiaohongshu.com)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Experience content to write (markdown)",
				},
			},
			"required": []string{"action"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			action, _ := input["action"].(string)
			switch action {
			case "list":
				domains := browser.ListSiteExperiences()
				if len(domains) == 0 {
					return "No site experiences stored yet.", nil
				}
				return "Stored site experiences:\n  " + strings.Join(domains, "\n  "), nil
			case "read":
				domain, _ := input["domain"].(string)
				if domain == "" {
					return "", fmt.Errorf("domain is required for read")
				}
				content, err := browser.ReadSiteExperience(domain)
				if err != nil {
					return fmt.Sprintf("No experience for %s yet.", domain), nil
				}
				return content, nil
			case "write":
				domain, _ := input["domain"].(string)
				content, _ := input["content"].(string)
				if domain == "" || content == "" {
					return "", fmt.Errorf("domain and content required for write")
				}
				if err := browser.WriteSiteExperience(domain, content); err != nil {
					return "", err
				}
				return fmt.Sprintf("Saved experience for %s", domain), nil
			default:
				return "", fmt.Errorf("unknown action: %s (use read, write, or list)", action)
			}
		},
	}
}

// browserJinaTool fetches a URL via Jina for Markdown conversion.
func browserJinaTool() *ToolSpec {
	return &ToolSpec{
		Name:        "browser_jina",
		Permission:  PermReadOnly,
		Description: "Fetch a URL via Jina (r.jina.ai) to get clean Markdown. Saves tokens vs full HTML. Best for articles, blogs, docs. 20 RPM limit.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL to fetch (will be prefixed with r.jina.ai/)",
				},
			},
			"required": []string{"url"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			u, _ := input["url"].(string)
			if u == "" {
				return "", fmt.Errorf("url is required")
			}
			// Strip protocol for Jina prefix
			u = strings.TrimPrefix(u, "https://")
			u = strings.TrimPrefix(u, "http://")
			jinaURL := "https://r.jina.ai/" + u
			return fmt.Sprintf("jina_url: %s\nUse WebFetch or curl to retrieve: %s", jinaURL, jinaURL), nil
		},
	}
}

// Suppress unused import warnings
var (
	_ = config.GlobalFlags
)
