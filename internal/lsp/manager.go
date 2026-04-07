package lsp

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"net/url"
)

// LspServerConfig mirrors Rust LspServerConfig — config for a single LSP server.
type LspServerConfig struct {
	Name                 string            `json:"name"`
	Command              string            `json:"command"`
	Args                 []string          `json:"args"`
	ExtensionToLanguage  map[string]string `json:"extension_to_language"`
}

// SymbolLocation mirrors Rust SymbolLocation — a resolved symbol location.
type SymbolLocation struct {
	Path  string `json:"path"`
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// FileDiagnostics mirrors Rust FileDiagnostics.
type FileDiagnostics struct {
	Path        string       `json:"path"`
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// WorkspaceDiagnostics mirrors Rust WorkspaceDiagnostics.
type WorkspaceDiagnostics struct {
	Files []FileDiagnostics `json:"files"`
}

// LspContextEnrichment mirrors Rust LspContextEnrichment.
type LspContextEnrichment struct {
	FilePath     string              `json:"file_path"`
	Diagnostics  WorkspaceDiagnostics `json:"diagnostics"`
	Definitions  []SymbolLocation    `json:"definitions"`
	References   []SymbolLocation    `json:"references"`
}

// NormalizeExtension normalizes a file extension for LSP lookup.
func NormalizeExtension(ext string) string {
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}

// LspManager mirrors Rust LspManager — manages multiple LSP server instances.
type LspManager struct {
	serverConfigs map[string]LspServerConfig
	extensionMap  map[string]string // normalized ext -> server name
	clients       map[string]*Client
	mu            sync.Mutex
}

// NewLspManager creates a new LSP manager from server configs.
// Mirrors Rust LspManager::new.
func NewLspManager(serverConfigs []LspServerConfig) (*LspManager, error) {
	configsByName := make(map[string]LspServerConfig)
	extMap := make(map[string]string)

	for _, config := range serverConfigs {
		for ext := range config.ExtensionToLanguage {
			normalized := NormalizeExtension(ext)
			if existing, ok := extMap[normalized]; ok {
				return nil, NewDuplicateExtensionError(normalized, existing, config.Name)
			}
			extMap[normalized] = config.Name
		}
		configsByName[config.Name] = config
	}

	return &LspManager{
		serverConfigs: configsByName,
		extensionMap:  extMap,
		clients:       make(map[string]*Client),
	}, nil
}

// SupportsPath returns whether any configured LSP server handles the given path.
// Mirrors Rust LspManager::supports_path.
func (m *LspManager) SupportsPath(path string) bool {
	ext := filepath.Ext(path)
	if ext == "" {
		return false
	}
	normalized := NormalizeExtension(ext)
	_, ok := m.extensionMap[normalized]
	return ok
}

// OpenDocument opens a document in the appropriate LSP server.
func (m *LspManager) OpenDocument(ctx context.Context, path, text string) error {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return err
	}
	uri := pathToURI(path)
	client.notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": m.languageForPath(path),
			"version":    1,
			"text":       text,
		},
	})
	return nil
}

// SyncDocumentFromDisk reads the file from disk and syncs with the LSP server.
func (m *LspManager) SyncDocumentFromDisk(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := m.ChangeDocument(ctx, path, string(data)); err != nil {
		return err
	}
	return m.SaveDocument(ctx, path)
}

// ChangeDocument notifies the LSP server of a document change.
func (m *LspManager) ChangeDocument(ctx context.Context, path, text string) error {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return err
	}
	uri := pathToURI(path)
	client.notify("textDocument/didChange", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":     uri,
			"version": 0,
		},
		"contentChanges": []map[string]interface{}{
			{"text": text},
		},
	})
	return nil
}

// SaveDocument notifies the LSP server of a document save.
func (m *LspManager) SaveDocument(ctx context.Context, path string) error {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return err
	}
	uri := pathToURI(path)
	client.notify("textDocument/didSave", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	})
	return nil
}

// CloseDocument notifies the LSP server of a document close.
func (m *LspManager) CloseDocument(ctx context.Context, path string) error {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return err
	}
	uri := pathToURI(path)
	client.notify("textDocument/didClose", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	})
	return nil
}

// GoToDefinition returns definition locations for a position in a file.
// Mirrors Rust LspManager::go_to_definition.
func (m *LspManager) GoToDefinition(ctx context.Context, path string, pos Position) ([]SymbolLocation, error) {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return nil, err
	}
	uri := pathToURI(path)
	locs, err := client.Definition(ctx, uri, pos.Line, pos.Character)
	if err != nil {
		return nil, err
	}
	result := make([]SymbolLocation, 0, len(locs))
	for _, loc := range locs {
		p := uriToPath(loc.URI)
		result = append(result, SymbolLocation{Path: p, URI: loc.URI, Range: loc.Range})
	}
	dedupeLocations(result)
	return result, nil
}

// FindReferences returns reference locations for a position in a file.
// Mirrors Rust LspManager::find_references.
func (m *LspManager) FindReferences(ctx context.Context, path string, pos Position, includeDeclaration bool) ([]SymbolLocation, error) {
	client, err := m.clientForPath(ctx, path)
	if err != nil {
		return nil, err
	}
	uri := pathToURI(path)
	locs, err := client.References(ctx, uri, pos.Line, pos.Character, includeDeclaration)
	if err != nil {
		return nil, err
	}
	result := make([]SymbolLocation, 0, len(locs))
	for _, loc := range locs {
		p := uriToPath(loc.URI)
		result = append(result, SymbolLocation{Path: p, URI: loc.URI, Range: loc.Range})
	}
	dedupeLocations(result)
	return result, nil
}

// CollectWorkspaceDiagnostics collects diagnostics from all active LSP servers.
// Mirrors Rust LspManager::collect_workspace_diagnostics.
func (m *LspManager) CollectWorkspaceDiagnostics(ctx context.Context) (WorkspaceDiagnostics, error) {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for _, c := range m.clients {
		clients = append(clients, c)
	}
	m.mu.Unlock()

	var files []FileDiagnostics
	for _, client := range clients {
		allDiags := client.AllDiagnostics()
		for uri, diags := range allDiags {
			if len(diags) == 0 {
				continue
			}
			files = append(files, FileDiagnostics{
				Path:        uriToPath(uri),
				URI:         uri,
				Diagnostics: diags,
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return WorkspaceDiagnostics{Files: files}, nil
}

// ContextEnrichment returns full context enrichment for a file position.
// Mirrors Rust LspManager::context_enrichment.
func (m *LspManager) ContextEnrichment(ctx context.Context, path string, pos Position) (LspContextEnrichment, error) {
	diagnostics, err := m.CollectWorkspaceDiagnostics(ctx)
	if err != nil {
		return LspContextEnrichment{}, err
	}
	definitions, err := m.GoToDefinition(ctx, path, pos)
	if err != nil {
		return LspContextEnrichment{}, err
	}
	references, err := m.FindReferences(ctx, path, pos, true)
	if err != nil {
		return LspContextEnrichment{}, err
	}
	return LspContextEnrichment{
		FilePath:    path,
		Diagnostics: diagnostics,
		Definitions: definitions,
		References:  references,
	}, nil
}

// Shutdown shuts down all LSP servers.
// Mirrors Rust LspManager::shutdown.
func (m *LspManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	drained := make([]*Client, 0, len(m.clients))
	for name, c := range m.clients {
		drained = append(drained, c)
		delete(m.clients, name)
	}
	m.mu.Unlock()

	var firstErr error
	for _, c := range drained {
		if err := c.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// clientForPath returns (or lazily creates) the LSP client for a given file path.
// Mirrors Rust LspManager::client_for_path.
func (m *LspManager) clientForPath(ctx context.Context, path string) (*Client, error) {
	ext := filepath.Ext(path)
	if ext == "" {
		return nil, NewLspError(LspErrUnsupportedDocument, path)
	}
	normalized := NormalizeExtension(ext)
	serverName, ok := m.extensionMap[normalized]
	if !ok {
		return nil, NewLspError(LspErrUnsupportedDocument, path)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[serverName]; ok {
		return client, nil
	}

	config, ok := m.serverConfigs[serverName]
	if !ok {
		return nil, NewLspError(LspErrUnknownServer, serverName)
	}

	client, err := NewClient(ctx, config.Command, config.Args)
	if err != nil {
		return nil, err
	}

	m.clients[serverName] = client
	return client, nil
}

func (m *LspManager) languageForPath(path string) string {
	ext := NormalizeExtension(filepath.Ext(path))
	for _, config := range m.serverConfigs {
		if lang, ok := config.ExtensionToLanguage[ext]; ok {
			return lang
		}
		// Also check with dot prefix
		if lang, ok := config.ExtensionToLanguage["."+ext]; ok {
			return lang
		}
	}
	return ""
}

// dedupeLocations removes duplicate locations in-place.
// Mirrors Rust dedupe_locations.
func dedupeLocations(locations []SymbolLocation) {
	seen := make(map[[5]interface{}]bool)
	writeIdx := 0
	for _, loc := range locations {
		key := [5]interface{}{
			loc.Path,
			loc.Range.Start.Line,
			loc.Range.Start.Character,
			loc.Range.End.Line,
			loc.Range.End.Character,
		}
		if !seen[key] {
			seen[key] = true
			locations[writeIdx] = loc
			writeIdx++
		}
	}
}

func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// Convert backslashes to forward slashes for URI
	abs = filepath.ToSlash(abs)
	return "file://" + abs
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if u.Scheme == "file" {
		return filepath.FromSlash(u.Path)
	}
	return uri
}
