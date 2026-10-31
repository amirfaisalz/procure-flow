// Package api exposes the embedded OpenAPI contract so the HTTP layer can
// serve it without a build step or filesystem access at runtime.
package api

import _ "embed"

// Spec is the canonical OpenAPI 3.1 document (api/openapi.yaml).
//
//go:embed openapi.yaml
var Spec []byte
