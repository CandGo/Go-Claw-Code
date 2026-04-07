package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TextFilePayload mirrors Rust TextFilePayload.
type TextFilePayload struct {
	FilePath    string `json:"filePath"`
	Content     string `json:"content"`
	NumLines    int    `json:"numLines"`
	StartLine   int    `json:"startLine"`
	TotalLines  int    `json:"totalLines"`
}

// ReadFileOutput mirrors Rust ReadFileOutput.
type ReadFileOutput struct {
	Kind string          `json:"type"`
	File TextFilePayload `json:"file"`
}

// StructuredPatchHunk mirrors Rust StructuredPatchHunk.
type StructuredPatchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// WriteFileOutput mirrors Rust WriteFileOutput.
type WriteFileOutput struct {
	Kind            string               `json:"type"`
	FilePath        string               `json:"filePath"`
	Content         string               `json:"content"`
	StructuredPatch []StructuredPatchHunk `json:"structuredPatch"`
	OriginalFile    *string              `json:"originalFile"`
	GitDiff         interface{}          `json:"gitDiff"`
}

// EditFileOutput mirrors Rust EditFileOutput.
type EditFileOutput struct {
	FilePath        string               `json:"filePath"`
	OldString       string               `json:"oldString"`
	NewString       string               `json:"newString"`
	OriginalFile    string               `json:"originalFile"`
	StructuredPatch []StructuredPatchHunk `json:"structuredPatch"`
	UserModified    bool                 `json:"userModified"`
	ReplaceAll      bool                 `json:"replaceAll"`
	GitDiff         interface{}          `json:"gitDiff"`
}

// GlobSearchOutput mirrors Rust GlobSearchOutput.
type GlobSearchOutput struct {
	DurationMs int64    `json:"durationMs"`
	NumFiles   int      `json:"numFiles"`
	Filenames  []string `json:"filenames"`
	Truncated  bool     `json:"truncated"`
}

// GrepSearchInput mirrors Rust GrepSearchInput.
type GrepSearchInput struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Glob            string `json:"glob,omitempty"`
	OutputMode      string `json:"output_mode,omitempty"`
	Before          *int   `json:"-B,omitempty"`
	After           *int   `json:"-A,omitempty"`
	ContextShort    *int   `json:"-C,omitempty"`
	Context         *int   `json:"context,omitempty"`
	LineNumbers     *bool  `json:"-n,omitempty"`
	CaseInsensitive *bool  `json:"-i,omitempty"`
	FileType        string `json:"type,omitempty"`
	HeadLimit       *int   `json:"head_limit,omitempty"`
	Offset          *int   `json:"offset,omitempty"`
	Multiline       *bool  `json:"multiline,omitempty"`
}

// GrepSearchOutput mirrors Rust GrepSearchOutput.
type GrepSearchOutput struct {
	Mode          string   `json:"mode,omitempty"`
	NumFiles      int      `json:"numFiles"`
	Filenames     []string `json:"filenames"`
	Content       *string  `json:"content,omitempty"`
	NumLines      *int     `json:"numLines,omitempty"`
	NumMatches    *int     `json:"numMatches,omitempty"`
	AppliedLimit  *int     `json:"appliedLimit,omitempty"`
	AppliedOffset *int     `json:"appliedOffset,omitempty"`
}

// ReadFile mirrors Rust read_file.
func ReadFile(path string, offset, limit *int) (*ReadFileOutput, error) {
	absPath, err := normalizePath(path)
	if err != nil {
		return nil, fmt.Errorf("normalize path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	// Remove trailing empty line from split if file ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	startIndex := 0
	if offset != nil && *offset > 0 {
		startIndex = *offset
	}
	if startIndex > len(lines) {
		startIndex = len(lines)
	}

	endIndex := len(lines)
	if limit != nil && *limit > 0 {
		endIndex = startIndex + *limit
	}
	if endIndex > len(lines) {
		endIndex = len(lines)
	}

	selected := strings.Join(lines[startIndex:endIndex], "\n")

	return &ReadFileOutput{
		Kind: "text",
		File: TextFilePayload{
			FilePath:   absPath,
			Content:    selected,
			NumLines:   endIndex - startIndex,
			StartLine:  startIndex + 1,
			TotalLines: len(lines),
		},
	}, nil
}

// WriteFile mirrors Rust write_file.
func WriteFile(path, content string) (*WriteFileOutput, error) {
	absPath, err := normalizePathAllowMissing(path)
	if err != nil {
		return nil, fmt.Errorf("normalize path: %w", err)
	}

	var originalFile *string
	if data, err := os.ReadFile(absPath); err == nil {
		s := string(data)
		originalFile = &s
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, fmt.Errorf("create dirs: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	kind := "create"
	if originalFile != nil {
		kind = "update"
	}

	origContent := ""
	if originalFile != nil {
		origContent = *originalFile
	}

	return &WriteFileOutput{
		Kind:            kind,
		FilePath:        absPath,
		Content:         content,
		StructuredPatch: makePatch(origContent, content),
		OriginalFile:    originalFile,
		GitDiff:         nil,
	}, nil
}

// EditFile mirrors Rust edit_file.
func EditFile(path, oldString, newString string, replaceAll bool) (*EditFileOutput, error) {
	absPath, err := normalizePath(path)
	if err != nil {
		return nil, fmt.Errorf("normalize path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	originalFile := string(data)

	if oldString == newString {
		return nil, fmt.Errorf("old_string and new_string must differ")
	}
	if !strings.Contains(originalFile, oldString) {
		return nil, fmt.Errorf("old_string not found in file")
	}

	var updated string
	if replaceAll {
		updated = strings.ReplaceAll(originalFile, oldString, newString)
	} else {
		updated = strings.Replace(originalFile, oldString, newString, 1)
	}

	if err := os.WriteFile(absPath, []byte(updated), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &EditFileOutput{
		FilePath:        absPath,
		OldString:       oldString,
		NewString:       newString,
		OriginalFile:    originalFile,
		StructuredPatch: makePatch(originalFile, updated),
		UserModified:    false,
		ReplaceAll:      replaceAll,
		GitDiff:         nil,
	}, nil
}

// GlobSearch mirrors Rust glob_search.
func GlobSearch(pattern string, path string) (*GlobSearchOutput, error) {
	started := time.Now()

	baseDir := path
	if baseDir == "" {
		baseDir, _ = os.Getwd()
	}
	if baseDir == "" {
		baseDir = "."
	}

	searchPattern := pattern
	if !filepath.IsAbs(pattern) {
		searchPattern = filepath.Join(baseDir, pattern)
	}

	var matches []string
	// Skip common non-project directories
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
		".svn": true, ".hg": true, ".next": true, ".nuxt": true,
		"dist": true, "build": true, "target": true, ".cache": true,
		".tox": true, ".mypy_cache": true, ".pytest_cache": true,
		"coverage": true, ".terraform": true,
	}
	filepath.WalkDir(baseDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(baseDir, p)
		if relErr != nil {
			return nil
		}
		matched := matchDoublestar(searchPattern, p)
		if !matched {
			// Try matching relative path
			matched = matchDoublestar(pattern, rel)
		}
		if matched {
			matches = append(matches, p)
		}
		return nil
	})

	// Sort by modification time (newest first)
	sort.Slice(matches, func(i, j int) bool {
		mi, _ := os.Stat(matches[i])
		mj, _ := os.Stat(matches[j])
		return mi.ModTime().After(mj.ModTime())
	})

	truncated := len(matches) > 100
	if len(matches) > 100 {
		matches = matches[:100]
	}

	elapsed := time.Since(started).Milliseconds()
	return &GlobSearchOutput{
		DurationMs: elapsed,
		NumFiles:   len(matches),
		Filenames:  matches,
		Truncated:  truncated,
	}, nil
}

// GrepSearch mirrors Rust grep_search.
func GrepSearch(input *GrepSearchInput) (*GrepSearchOutput, error) {
	basePath := input.Path
	if basePath == "" {
		basePath, _ = os.Getwd()
	}
	if basePath == "" {
		basePath = "."
	}

	flags := ""
	if input.CaseInsensitive != nil && *input.CaseInsensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + input.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	outputMode := input.OutputMode
	if outputMode == "" {
		outputMode = "files_with_matches"
	}
	context := 0
	if input.Context != nil && *input.Context > 0 {
		context = *input.Context
	}
	if input.ContextShort != nil && *input.ContextShort > 0 {
		context = *input.ContextShort
	}

	var filenames []string
	var contentLines []string
	totalMatches := 0

	searchFiles, _ := collectSearchFiles(basePath)
	for _, filePath := range searchFiles {
		if !matchesOptionalFilters(filePath, input.Glob, input.FileType) {
			continue
		}

		fileData, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		fileContents := string(fileData)

		if outputMode == "count" {
			count := len(re.FindAllStringIndex(fileContents, -1))
			if count > 0 {
				filenames = append(filenames, filePath)
				totalMatches += count
			}
			continue
		}

		lines := strings.Split(fileContents, "\n")
		var matchedIndices []int
		for i, line := range lines {
			if re.MatchString(line) {
				totalMatches++
				matchedIndices = append(matchedIndices, i)
			}
		}

		if len(matchedIndices) == 0 {
			continue
		}

		filenames = append(filenames, filePath)
		if outputMode == "content" {
			before := context
			after := context
			if input.Before != nil {
				before = *input.Before
			}
			if input.After != nil {
				after = *input.After
			}

			for _, idx := range matchedIndices {
				start := idx - before
				if start < 0 {
					start = 0
				}
				end := idx + after + 1
				if end > len(lines) {
					end = len(lines)
				}
				showLineNumbers := true
				if input.LineNumbers != nil {
					showLineNumbers = *input.LineNumbers
				}
				for j := start; j < end; j++ {
					if showLineNumbers {
						contentLines = append(contentLines, fmt.Sprintf("%s:%d:%s", filePath, j+1, lines[j]))
					} else {
						contentLines = append(contentLines, fmt.Sprintf("%s:%s", filePath, lines[j]))
					}
				}
			}
		}
	}

	limit := 250
	if input.HeadLimit != nil {
		limit = *input.HeadLimit
	}
	offsetVal := 0
	if input.Offset != nil {
		offsetVal = *input.Offset
	}

	// Apply offset and limit to filenames
	if offsetVal > 0 && offsetVal < len(filenames) {
		filenames = filenames[offsetVal:]
	}
	appliedLimit := (*int)(nil)
	if len(filenames) > limit {
		filenames = filenames[:limit]
		appliedLimit = &limit
	}
	appliedOffset := (*int)(nil)
	if offsetVal > 0 {
		appliedOffset = &offsetVal
	}

	if outputMode == "content" {
		if offsetVal > 0 && offsetVal < len(contentLines) {
			contentLines = contentLines[offsetVal:]
		}
		var contentAppliedLimit *int
		if len(contentLines) > limit {
			contentLines = contentLines[:limit]
			contentAppliedLimit = &limit
		}
		content := strings.Join(contentLines, "\n")
		return &GrepSearchOutput{
			Mode:          outputMode,
			NumFiles:      len(filenames),
			Filenames:     filenames,
			Content:       &content,
			NumLines:      intPtr(len(contentLines)),
			NumMatches:    nil,
			AppliedLimit:  contentAppliedLimit,
			AppliedOffset: appliedOffset,
		}, nil
	}

	result := &GrepSearchOutput{
		Mode:          outputMode,
		NumFiles:      len(filenames),
		Filenames:     filenames,
		NumLines:      nil,
		AppliedLimit:  appliedLimit,
		AppliedOffset: appliedOffset,
	}
	if outputMode == "count" {
		result.NumMatches = &totalMatches
	}
	return result, nil
}

// collectSearchFiles mirrors Rust collect_search_files.
func collectSearchFiles(basePath string) ([]string, error) {
	info, err := os.Stat(basePath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", basePath, err)
	}
	if !info.IsDir() {
		return []string{basePath}, nil
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
		".svn": true, ".hg": true, ".next": true, ".nuxt": true,
		"dist": true, "build": true, "target": true, ".cache": true,
		".tox": true, ".mypy_cache": true, ".pytest_cache": true,
		"coverage": true, ".terraform": true,
	}

	var files []string
	filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, nil
}

// matchesOptionalFilters mirrors Rust matches_optional_filters.
func matchesOptionalFilters(path, globFilter, fileType string) bool {
	if globFilter != "" {
		matched, _ := filepath.Match(globFilter, filepath.Base(path))
		if !matched {
			matched = matchDoublestar(globFilter, path)
		}
		if !matched {
			return false
		}
	}

	if fileType != "" {
		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		if ext != fileType {
			return false
		}
	}

	return true
}

// makePatch mirrors Rust make_patch.
func makePatch(original, updated string) []StructuredPatchHunk {
	var lines []string
	for _, line := range strings.Split(original, "\n") {
		if line == "" && strings.HasSuffix(original, "\n") {
			continue
		}
		lines = append(lines, "-"+line)
	}
	for _, line := range strings.Split(updated, "\n") {
		if line == "" && strings.HasSuffix(updated, "\n") {
			continue
		}
		lines = append(lines, "+"+line)
	}

	oldLines := len(strings.Split(original, "\n"))
	newLines := len(strings.Split(updated, "\n"))
	if original == "" {
		oldLines = 0
	}
	if updated == "" {
		newLines = 0
	}

	return []StructuredPatchHunk{
		{
			OldStart: 1,
			OldLines: oldLines,
			NewStart: 1,
			NewLines: newLines,
			Lines:    lines,
		},
	}
}

// normalizePath resolves a path to absolute, canonical form.
// Mirrors Rust normalize_path.
func normalizePath(path string) (string, error) {
	var candidate string
	if filepath.IsAbs(path) {
		candidate = path
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		candidate = filepath.Join(cwd, path)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	// Try to canonicalize (resolve symlinks)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// normalizePathAllowMissing mirrors Rust normalize_path_allow_missing.
func normalizePathAllowMissing(path string) (string, error) {
	var candidate string
	if filepath.IsAbs(path) {
		candidate = path
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		candidate = filepath.Join(cwd, path)
	}

	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		return resolved, nil
	}

	parent := filepath.Dir(candidate)
	if parent != "" && parent != candidate {
		canonicalParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			canonicalParent = parent
		}
		name := filepath.Base(candidate)
		return filepath.Join(canonicalParent, name), nil
	}

	return candidate, nil
}

// intPtr returns a pointer to the given int.
func intPtr(v int) *int { return &v }

// matchDoublestar provides proper ** glob matching without external dependencies.
// It handles:
//   - ** matches zero or more path segments
//   - **/*.go matches all .go files at any depth
//   - src/** matches everything under src/
//   - **/test/** matches any path containing a test directory
//   - Regular glob chars: * (any non-separator), ? (single char), [...] (char class)
//
// The function works by splitting the pattern on "**" into segments, then
// recursively matching each segment against path components.
func matchDoublestar(pattern, name string) bool {
	// Normalize separators to /
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)

	// Remove leading "./" from both
	if strings.HasPrefix(pattern, "./") {
		pattern = pattern[2:]
	}
	if strings.HasPrefix(name, "./") {
		name = name[2:]
	}

	return matchDoublestarSegments(strings.Split(pattern, "**"), strings.Split(name, "/"))
}

// matchDoublestarSegments matches a path (split into components) against
// a pattern that has been split on "**". Each "**" in the original pattern
// corresponds to a boundary between segments, where zero or more path
// components may be consumed.
func matchDoublestarSegments(segments []string, components []string) bool {
	if len(segments) == 0 {
		return len(components) == 0
	}

	// First segment must match from the beginning
	first := segments[0]
	rest := segments[1:]

	// Trim leading/trailing slashes from the first segment parts
	firstParts := splitPatternParts(first)

	// Match the first segment parts against leading components
	matchedCount, ok := matchPatternParts(firstParts, components, 0)
	if !ok {
		return false
	}

	if len(rest) == 0 {
		// No more segments; all components must have been consumed
		return matchedCount == len(components)
	}

	// After the first segment, we have a "**" boundary.
	// The next segment can match at any position from matchedCount onward.
	return matchDoublestarGlob(rest, components, matchedCount)
}

// matchDoublestarGlob handles the recursive matching after a "**" boundary.
// The next pattern segment can match starting at any position in components
// from pos onward (including pos, meaning ** can match zero components).
func matchDoublestarGlob(segments []string, components []string, pos int) bool {
	if len(segments) == 0 {
		// All segments consumed; remaining components are matched by the trailing **
		return true
	}

	segment := segments[0]
	rest := segments[1:]

	// Split this segment into pattern parts (by /)
	parts := splitPatternParts(segment)

	// Try matching this segment at every position from pos onward
	for i := pos; i <= len(components); i++ {
		matchedCount, ok := matchPatternParts(parts, components, i)
		if ok {
			if len(rest) == 0 {
				// This is the last segment; all remaining components must be consumed
				if matchedCount == len(components) {
					return true
				}
				// If the pattern ends with a trailing ** (segment is empty or "/"),
				// remaining components are allowed
				if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
					return true
				}
			} else {
				if matchDoublestarGlob(rest, components, matchedCount) {
					return true
				}
			}
		}
	}

	return false
}

// splitPatternParts splits a pattern segment by "/" and filters empty strings
// that come from leading/trailing slashes, but preserves empty parts that
// represent structure (e.g., "a//b" is unusual but we handle it).
func splitPatternParts(segment string) []string {
	if segment == "" {
		return nil
	}
	parts := strings.Split(segment, "/")
	// Remove trailing empty parts (from trailing /)
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	// Remove leading empty parts (from leading /)
	for len(parts) > 0 && parts[0] == "" {
		parts = parts[1:]
	}
	return parts
}

// matchPatternParts matches a sequence of glob pattern parts against path
// components starting at offset. Returns the number of components consumed
// and whether the match succeeded.
func matchPatternParts(parts []string, components []string, offset int) (int, bool) {
	if len(parts) == 0 {
		return offset, true
	}
	if offset+len(parts) > len(components) {
		return offset, false
	}

	for i, part := range parts {
		if !singleGlobMatch(part, components[offset+i]) {
			return offset, false
		}
	}

	return offset + len(parts), true
}

// singleGlobMatch matches a single glob pattern against a single path component.
// Supports: * (any non-separator chars), ? (single char), [abc] and [a-z] (char classes).
func singleGlobMatch(pattern, name string) bool {
	// Fast path: exact match
	if pattern == name {
		return true
	}
	// Fast path: no special characters
	if !strings.ContainsAny(pattern, "*?[") {
		return pattern == name
	}

	return globMatchRunes(pattern, name)
}

// globMatchRunes performs rune-by-rune glob matching.
func globMatchRunes(pattern, name string) bool {
	pi := 0
	ni := 0
	starPi := -1
	starNi := -1

	for ni < len(name) {
		if pi < len(pattern) {
			pc, pcSize := utf8RuneAt(pattern, pi)

			switch {
			case pc == '*':
				// Remember the star position and try matching zero characters first
				starPi = pi
				starNi = ni
				pi += pcSize
				continue

			case pc == '[':
				// Character class
				end := findCharClassEnd(pattern, pi+pcSize)
				if end > pi {
					pr, prSize := utf8RuneAt(name, ni)
					if charClassMatch(pattern[pi:end+1], pr) {
						pi = end + 1
						ni += prSize
						continue
					}
				}
				// Invalid char class, treat [ as literal
				pr, prSize := utf8RuneAt(name, ni)
				if pc == pr {
					pi += pcSize
					ni += prSize
					continue
				}

			case pc == '?':
				_, prSize := utf8RuneAt(name, ni)
				pi += pcSize
				ni += prSize
				continue

			default:
				pr, prSize := utf8RuneAt(name, ni)
				if pc == pr {
					pi += pcSize
					ni += prSize
					continue
				}
			}
		}

		// If we have a star to backtrack to, try consuming one more character
		if starPi >= 0 {
			pi = starPi + 1 // skip past the *
			_, rSize := utf8RuneAt(name, starNi)
			starNi += rSize
			ni = starNi
			continue
		}

		return false
	}

	// Consume trailing *s in pattern
	for pi < len(pattern) {
		pc, pcSize := utf8RuneAt(pattern, pi)
		if pc != '*' {
			break
		}
		pi += pcSize
	}

	return pi >= len(pattern)
}

// utf8RuneAt decodes the rune at position i in s, returning the rune and its size.
func utf8RuneAt(s string, i int) (rune, int) {
	if i >= len(s) {
		return utf8RuneError, 1
	}
	r, size := rune(s[i]), 1
	if s[i] >= utf8RuneSelf {
		r, size = decodeUTF8(s[i:])
	}
	return r, size
}

const (
	utf8RuneError = '\ufffd'
	utf8RuneSelf  = 0x80
)

// decodeUTF8 decodes the first UTF-8 rune from b.
func decodeUTF8(b string) (rune, int) {
	if len(b) == 0 {
		return utf8RuneError, 0
	}
	n := len(b)
	if n > 4 {
		n = 4
	}
	for i := n; i > 0; i-- {
		r := []byte(b[:i])
		var rn rune
		cnt := 0
		// Manual decode
		if len(r) > 0 {
			if r[0] < 0x80 {
				rn = rune(r[0])
				cnt = 1
			} else if len(r) >= 2 && r[0] < 0xE0 {
				rn = rune(r[0]&0x1F)<<6 | rune(r[1]&0x3F)
				cnt = 2
			} else if len(r) >= 3 && r[0] < 0xF0 {
				rn = rune(r[0]&0x0F)<<12 | rune(r[1]&0x3F)<<6 | rune(r[2]&0x3F)
				cnt = 3
			} else if len(r) >= 4 {
				rn = rune(r[0]&0x07)<<18 | rune(r[1]&0x3F)<<12 | rune(r[2]&0x3F)<<6 | rune(r[3]&0x3F)
				cnt = 4
			}
		}
		if cnt > 0 {
			return rn, cnt
		}
	}
	return utf8RuneError, 1
}

// findCharClassEnd finds the closing ] for a character class starting after [.
func findCharClassEnd(pattern string, start int) int {
	i := start
	if i < len(pattern) && pattern[i] == '!' {
		i++
	}
	if i < len(pattern) && pattern[i] == ']' {
		i++
	}
	for i < len(pattern) {
		if pattern[i] == ']' {
			return i
		}
		i++
	}
	return -1
}

// charClassMatch checks if a rune matches a character class pattern like [abc] or [a-z].
func charClassMatch(pattern string, r rune) bool {
	if len(pattern) < 2 || pattern[0] != '[' || pattern[len(pattern)-1] != ']' {
		return false
	}

	negated := false
	content := pattern[1 : len(pattern)-1]
	if len(content) > 0 && content[0] == '!' {
		negated = true
		content = content[1:]
	}

	matched := charClassContentMatch(content, r)
	if negated {
		return !matched
	}
	return matched
}

// charClassContentMatch checks if a rune matches the content of a character class.
func charClassContentMatch(content string, r rune) bool {
	i := 0
	for i < len(content) {
		// Check for range: a-z
		if i+2 < len(content) && content[i+1] == '-' {
			startR := rune(content[i])
			endR := rune(content[i+2])
			if r >= startR && r <= endR {
				return true
			}
			i += 3
			continue
		}
		if rune(content[i]) == r {
			return true
		}
		i++
	}
	return false
}
