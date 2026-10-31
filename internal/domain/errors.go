package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors shared by all domain modules. Transport adapters translate
// these errors into protocol-specific responses (for example HTTP status codes).
var (
	ErrNotFound          = errors.New("resource not found")
	ErrConflict          = errors.New("resource conflict")
	ErrInvalidTransition = errors.New("invalid status transition")
)

// ValidationError describes one or more invalid input fields.
type ValidationError struct {
	Fields map[string]string
}

// Add records a validation message for a field.
func (e *ValidationError) Add(field, message string) {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[field] = message
}

// OrNil returns nil when no field errors were recorded.
func (e *ValidationError) OrNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

// Error renders a deterministic message so it is safe to log and compare.
func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}

	fields := make([]string, 0, len(e.Fields))
	for field := range e.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, fmt.Sprintf("%s: %s", field, e.Fields[field]))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
