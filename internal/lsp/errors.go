package lsp

import (
	"fmt"
)

// LspError mirrors Rust LspError — the error type for LSP operations.
type LspError struct {
	Kind    LspErrorKind
	Message string
	Err     error
}

type LspErrorKind int

const (
	LspErrIO                  LspErrorKind = iota
	LspErrJSON
	LspErrInvalidHeader
	LspErrMissingContentLength
	LspErrInvalidContentLength
	LspErrUnsupportedDocument
	LspErrUnknownServer
	LspErrDuplicateExtension
	LspErrPathToURL
	LspErrProtocol
)

func (e *LspError) Error() string {
	switch e.Kind {
	case LspErrIO:
		return e.Err.Error()
	case LspErrJSON:
		return e.Err.Error()
	case LspErrInvalidHeader:
		return fmt.Sprintf("invalid LSP header: %s", e.Message)
	case LspErrMissingContentLength:
		return "missing LSP Content-Length header"
	case LspErrInvalidContentLength:
		return fmt.Sprintf("invalid LSP Content-Length value: %s", e.Message)
	case LspErrUnsupportedDocument:
		return fmt.Sprintf("no LSP server configured for %s", e.Message)
	case LspErrUnknownServer:
		return fmt.Sprintf("unknown LSP server: %s", e.Message)
	case LspErrDuplicateExtension:
		return e.Message
	case LspErrPathToURL:
		return fmt.Sprintf("failed to convert path to file URL: %s", e.Message)
	case LspErrProtocol:
		return fmt.Sprintf("LSP protocol error: %s", e.Message)
	default:
		return e.Message
	}
}

func (e *LspError) Unwrap() error { return e.Err }

// LspErrorf creates an LspError with a format message.
func LspErrorf(kind LspErrorKind, format string, args ...interface{}) *LspError {
	return &LspError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// NewLspError creates an LspError with a plain message string.
func NewLspError(kind LspErrorKind, message string) *LspError {
	return &LspError{Kind: kind, Message: message}
}

// LspErrorWrap wraps an error as an LspError.
func LspErrorWrap(kind LspErrorKind, err error) *LspError {
	return &LspError{Kind: kind, Err: err}
}

// NewDuplicateExtensionError creates a DuplicateExtension error.
func NewDuplicateExtensionError(extension, existingServer, newServer string) *LspError {
	return &LspError{
		Kind:    LspErrDuplicateExtension,
		Message: fmt.Sprintf("duplicate LSP extension mapping for %s: %s and %s", extension, existingServer, newServer),
	}
}
