package httpapi

import (
	"net/http"

	apispec "github.com/amirfaisalz/procure-flow/api"
)

// docsCSP is scoped to the documentation page. It allows the Scalar bundle and
// its fonts while keeping everything else locked down. The API routes keep the
// stricter CSP from middleware.SecurityHeaders.
const docsCSP = "default-src 'none'; " +
	"script-src 'unsafe-inline' https://cdn.jsdelivr.net; " +
	"style-src 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.scalar.com; " +
	"font-src https://cdn.jsdelivr.net https://fonts.scalar.com data:; " +
	"img-src 'self' data: https:; " +
	"connect-src 'self' https:; " +
	"worker-src blob:; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'"

const docsHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>procure-flow API Reference</title>
  <style>
    body { margin: 0; }
  </style>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  <script>
    Scalar.createApiReference('#app', {
      url: '/openapi.yaml',
      theme: 'default',
      telemetry: false,
      hideModels: false,
      defaultHttpClient: {
        targetKey: 'shell',
        clientKey: 'curl'
      }
    })
  </script>
</body>
</html>
`

// docsHandler serves the interactive API reference and the raw OpenAPI
// document. It is registered only when config.Docs.Enabled is true.
type docsHandler struct{}

func newDocsHandler() *docsHandler {
	return &docsHandler{}
}

// Index serves GET /docs with a Scalar API reference. Scalar includes an
// in-browser request client, so the contract can be exercised directly.
func (h *docsHandler) Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", docsCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(docsHTML))
}

// Spec serves GET /openapi.yaml with the embedded contract.
func (h *docsHandler) Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(apispec.Spec)
}
