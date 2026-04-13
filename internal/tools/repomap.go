package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// RepoMap tool — Aider-style repository map generation
// Walks the directory tree, identifies key files, and extracts symbol
// definitions using regex parsing. Outputs a compact tree for context.
// ---------------------------------------------------------------------------

// RepoMap walks the directory tree and produces a compact symbol map.
func RepoMap(rootDir string, depth, fileLimit *int) (string, int, int, int, bool, int64, error) {
	started := time.Now()

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", 0, 0, 0, false, 0, fmt.Errorf("abs path: %w", err)
	}

	maxDepth := 6
	if depth != nil && *depth > 0 {
		maxDepth = *depth
	}
	maxFiles := 200
	if fileLimit != nil && *fileLimit > 0 {
		maxFiles = *fileLimit
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
		".svn": true, ".hg": true, ".next": true, ".nuxt": true,
		"dist": true, "build": true, "target": true, ".cache": true,
		".tox": true, ".mypy_cache": true, ".pytest_cache": true,
		"coverage": true, ".terraform": true,
		".idea": true, ".vscode": true, ".vs": true,
		"bin": true, "obj": true, "out": true,
	}

	var files []repoFileEntry
	totalSourceFiles := 0
	truncated := false

	filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return nil
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !isRepoMapSource(ext) {
			return nil
		}

		// Depth check
		depthCount := strings.Count(rel, string(filepath.Separator))
		if depthCount >= maxDepth {
			return nil
		}

		totalSourceFiles++

		if len(files) >= maxFiles {
			truncated = true
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		rf := repoFileEntry{RelPath: rel}
		contentStr := string(content)

		if ext == ".go" {
			rf.PkgName, rf.Symbols = extractGoSymbols(contentStr)
		} else if isRepoMapExtractable(ext) {
			rf.Symbols = extractGenericSymbols(contentStr, ext)
		}

		files = append(files, rf)
		return nil
	})

	mapStr := formatRepoMap(files)
	elapsed := time.Since(started).Milliseconds()
	estTokens := len(mapStr) * 2 / 7

	return mapStr, len(files), totalSourceFiles, estTokens, truncated, elapsed, nil
}

// --- Source file classification ---

var repoMapSourceExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".rs": true, ".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".md": true, ".yaml": true, ".yml": true, ".json": true, ".toml": true,
	".mod": true, ".sum": true,
}

var repoMapExtractableExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".rs": true, ".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
}

func isRepoMapSource(ext string) bool    { return repoMapSourceExts[ext] }
func isRepoMapExtractable(ext string) bool { return repoMapExtractableExts[ext] }

// --- Go symbol extraction (pre-compiled regexes) ---

var (
	reGoPackage = regexp.MustCompile(`^package\s+(\w+)`)
	reGoType    = regexp.MustCompile(`^type\s+(\w+)\s*(struct|interface)?`)
	reGoFunc    = regexp.MustCompile(`^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(([^)]*)\)`)
	reGoVar     = regexp.MustCompile(`^var\s+(\w+)`)
	reGoConst   = regexp.MustCompile(`^const\s+(\w+)`)
)

func extractGoSymbols(content string) (pkgName string, symbols []string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		if m := reGoPackage.FindStringSubmatch(trimmed); m != nil {
			pkgName = m[1]
			continue
		}
		if m := reGoType.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			kind := "type"
			if m[2] != "" {
				kind = m[2]
			}
			symbols = append(symbols, fmt.Sprintf("%s %s", kind, name))
			continue
		}
		if m := reGoFunc.FindStringSubmatch(trimmed); m != nil {
			sig := extractGoFuncSignature(trimmed)
			symbols = append(symbols, "func "+sig)
			continue
		}
		if m := reGoVar.FindStringSubmatch(trimmed); m != nil {
			if isGoExported(m[1]) {
				symbols = append(symbols, "var "+m[1])
			}
			continue
		}
		if m := reGoConst.FindStringSubmatch(trimmed); m != nil {
			if isGoExported(m[1]) {
				symbols = append(symbols, "const "+m[1])
			}
		}
	}
	return pkgName, symbols
}

func extractGoFuncSignature(line string) string {
	after := line
	if strings.HasPrefix(after, "func ") {
		after = after[5:]
	}
	// Skip receiver if present
	if strings.HasPrefix(after, "(") {
		depth := 1
		i := 1
		for ; i < len(after) && depth > 0; i++ {
			if after[i] == '(' {
				depth++
			}
			if after[i] == ')' {
				depth--
			}
		}
		after = strings.TrimLeft(after[i:], " ")
	}
	if idx := strings.Index(after, "{"); idx > 0 {
		after = after[:idx]
	}
	return strings.TrimRight(after, " \t")
}

func isGoExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// --- Generic symbol extraction (Python, JS/TS, Rust, Java, C/C++) ---

var (
	rePyFunc  = regexp.MustCompile(`^(async\s+)?def\s+(\w+)\s*\(([^)]*)\)`)
	rePyClass = regexp.MustCompile(`^class\s+(\w+)(?:\([^)]*\))?`)

	reJsFunc  = regexp.MustCompile(`^(export\s+)?(async\s+)?function\s+(\w+)\s*\(([^)]*)\)`)
	reJsClass = regexp.MustCompile(`^(export\s+)?(default\s+)?class\s+(\w+)`)
	reJsArrow = regexp.MustCompile(`^(export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)\s*=>`)

	reRsFn     = regexp.MustCompile(`^(pub\s+)?(?:async\s+)?fn\s+(\w+)\s*[<(\[]`)
	reRsStruct = regexp.MustCompile(`^(pub\s+)?struct\s+(\w+)`)
	reRsEnum   = regexp.MustCompile(`^(pub\s+)?enum\s+(\w+)`)
	reRsTrait  = regexp.MustCompile(`^(pub\s+)?trait\s+(\w+)`)
	reRsImpl   = regexp.MustCompile(`^impl\s+(?:<[^>]+>\s*)?(\w+)`)

	reJavaClass  = regexp.MustCompile(`^(public\s+|private\s+|protected\s+)?(static\s+)?(?:abstract\s+)?class\s+(\w+)`)
	reJavaIface  = regexp.MustCompile(`^(public\s+)?interface\s+(\w+)`)
	reJavaMethod = regexp.MustCompile(`^\s+(public|private|protected)\s+(?:static\s+)?(?:[\w<>\[\]]+\s+)+(\w+)\s*\(`)

	reCFn     = regexp.MustCompile(`^(?:[\w:*]+\s+)+(\w+)\s*\([^;]*\)\s*\{`)
	reCStruct = regexp.MustCompile(`^(?:typedef\s+)?struct\s+(\w+)`)
	reCClass  = regexp.MustCompile(`^class\s+(\w+)`)
)

func extractGenericSymbols(content string, ext string) []string {
	var symbols []string
	lines := strings.Split(content, "\n")

	switch ext {
	case ".py":
		for _, line := range lines {
			if m := rePyClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "class "+m[1])
			} else if m := rePyFunc.FindStringSubmatch(line); m != nil {
				prefix := ""
				if m[1] != "" {
					prefix = "async "
				}
				symbols = append(symbols, prefix+"func "+m[2]+"("+m[3]+")")
			}
		}
	case ".js", ".jsx", ".ts", ".tsx":
		for _, line := range lines {
			if m := reJsClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "class "+m[3])
			} else if m := reJsFunc.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "func "+m[3]+"("+m[4]+")")
			} else if m := reJsArrow.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "const "+m[2]+" = () =>")
			}
		}
	case ".rs":
		for _, line := range lines {
			if m := reRsStruct.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "struct "+m[2])
			} else if m := reRsEnum.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "enum "+m[2])
			} else if m := reRsTrait.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "trait "+m[2])
			} else if m := reRsImpl.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "impl "+m[1])
			} else if m := reRsFn.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "func "+m[2])
			}
		}
	case ".java":
		for _, line := range lines {
			if m := reJavaClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "class "+m[3])
			} else if m := reJavaIface.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "interface "+m[2])
			} else if m := reJavaMethod.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "method "+m[2])
			}
		}
	case ".c", ".cpp", ".h", ".hpp":
		for _, line := range lines {
			if m := reCStruct.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "struct "+m[1])
			} else if m := reCClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "class "+m[1])
			} else if m := reCFn.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, "func "+m[1])
			}
		}
	}
	return symbols
}

// --- Format output as tree ---

type repoFileEntry struct {
	RelPath string
	Symbols []string
	PkgName string
}

func formatRepoMap(entries []repoFileEntry) string {
	var buf strings.Builder

	// Group files by directory
	dirs := make(map[string][]repoFileEntry)
	var dirOrder []string
	for _, f := range entries {
		dir := filepath.Dir(f.RelPath)
		if dir == "." {
			dir = ""
		}
		if _, exists := dirs[dir]; !exists {
			dirOrder = append(dirOrder, dir)
		}
		dirs[dir] = append(dirs[dir], f)
	}

	for _, dir := range dirOrder {
		if dir != "" {
			fmt.Fprintf(&buf, "%s/\n", dir)
		}
		for _, f := range dirs[dir] {
			fname := filepath.Base(f.RelPath)
			if f.PkgName != "" {
				fmt.Fprintf(&buf, "  %s [pkg: %s]\n", fname, f.PkgName)
			} else {
				fmt.Fprintf(&buf, "  %s\n", fname)
			}
			for _, sym := range f.Symbols {
				fmt.Fprintf(&buf, "    %s\n", sym)
			}
		}
	}

	return buf.String()
}

// repoMapTool returns the tool spec for the RepoMap tool.
func repoMapTool() *ToolSpec {
	return &ToolSpec{
		Name:        "RepoMap",
		Permission:  PermReadOnly,
		Description: "Generate a compact map of the project's source code structure showing files, packages, types, functions, and interfaces.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Root directory to map. Defaults to current working directory.",
				},
				"depth": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum directory depth to traverse (default: 6)",
				},
				"file_limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of files to process (default: 200)",
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["path"].(string)
			if path == "" {
				path, _ = os.Getwd()
			}

			var depth *int
			if d, ok := input["depth"].(float64); ok && d > 0 {
				v := int(d)
				depth = &v
			}

			var fileLimit *int
			if fl, ok := input["file_limit"].(float64); ok && fl > 0 {
				v := int(fl)
				fileLimit = &v
			}

			mapStr, shown, total, tokens, trunc, ms, err := RepoMap(path, depth, fileLimit)
			if err != nil {
				return "", err
			}

			var buf strings.Builder
			fmt.Fprintf(&buf, "RepoMap: %s (%d/%d files, ~%d tokens", path, shown, total, tokens)
			if trunc {
				buf.WriteString(", truncated")
			}
			fmt.Fprintf(&buf, ", %dms)\n\n%s", ms, mapStr)
			return buf.String(), nil
		},
	}
}
