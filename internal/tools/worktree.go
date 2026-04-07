package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func worktreeCreateTool() *ToolSpec {
	return &ToolSpec{
		Name:        "EnterWorktree",
		Permission:  PermWorkspaceWrite,
		Description: "Create an isolated git worktree and switch the current session into it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name for the worktree (optional, auto-generated if omitted)",
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			name, _ := input["name"].(string)
			if name == "" {
				name = fmt.Sprintf("wt-%d", os.Getpid())
			}

			// Create worktree directory
			wtDir := fmt.Sprintf(".claude/worktrees/%s", name)
			os.MkdirAll(".claude/worktrees", 0755)

			// Create a new branch and worktree
			branchName := "worktree/" + name
			cmd := exec.Command("git", "worktree", "add", wtDir, "-b", branchName, "HEAD")
			out, err := cmd.CombinedOutput()
			if err != nil {
				// Try without creating a new branch (may already exist)
				cmd = exec.Command("git", "worktree", "add", wtDir, branchName)
				out, err = cmd.CombinedOutput()
				if err != nil {
					return "", fmt.Errorf("failed to create worktree: %s: %w", string(out), err)
				}
			}

			return fmt.Sprintf("<worktree_result>\n  <path>%s</path>\n  <branch>%s</branch>\n  <status>created</status>\n</worktree_result>",
				wtDir, branchName), nil
		},
	}
}

func worktreeRemoveTool() *ToolSpec {
	return &ToolSpec{
		Name:        "ExitWorktree",
		Permission:  PermWorkspaceWrite,
		Description: "Exit and optionally remove a git worktree session.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"keep", "remove"},
					"description": "keep = leave the worktree on disk, remove = delete it",
				},
				"discard_changes": map[string]interface{}{
					"type":        "boolean",
					"description": "Set to true to remove even with uncommitted changes",
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			action, _ := input["action"].(string)
			if action == "" {
				action = "keep"
			}

			if action == "keep" {
				return "<worktree_result>\n  <status>kept</status>\n</worktree_result>", nil
			}

			// Remove the worktree
			discardChanges := false
			if v, ok := input["discard_changes"].(bool); ok {
				discardChanges = v
			}

			// Find the current worktree
			out, err := exec.Command("git", "worktree", "list", "--porcelain").CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("failed to list worktrees: %w", err)
			}

			// Parse worktree list to find current directory
			cwd, _ := os.Getwd()
			var wtPath string
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "worktree ") {
					path := strings.TrimPrefix(line, "worktree ")
					if strings.Contains(cwd, path) && path != "." {
						wtPath = path
					}
				}
			}

			if wtPath == "" {
				return "<worktree_result>\n  <status>no_worktree</status>\n</worktree_result>", nil
			}

			args := []string{"worktree", "remove", wtPath}
			if discardChanges {
				args = append(args, "--force")
			}

			out, err = exec.Command("git", args...).CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("failed to remove worktree: %s: %w", string(out), err)
			}

			return fmt.Sprintf("<worktree_result>\n  <path>%s</path>\n  <status>removed</status>\n</worktree_result>", wtPath), nil
		},
	}
}
