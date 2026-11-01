package httpapi

import (
	"bytes"
	"net/http"

	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/middleware"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// fallback converts the ServeMux's plain-text 404/405 responses into RFC 9457
// problems. Go's ServeMux only reports a method mismatch through the response
// itself, so unmatched requests are buffered before the problem is written.
type fallback struct {
	mux *http.ServeMux
}

func (f *fallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, pattern := f.mux.Handler(r)
	if pattern != "" {
		// Route through the mux (not handler) so PathValue is populated.
		f.mux.ServeHTTP(w, r)
		// A nested mux (for example the authenticated /api/v1 sub-router)
		// matched a more specific pattern; report it to the metrics recorder.
		middleware.SetRoute(r.Context(), r.Pattern)
		return
	}

	rec := &bufferedWriter{header: make(http.Header)}
	handler.ServeHTTP(rec, r)

	if rec.status == http.StatusMethodNotAllowed {
		if allow := rec.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
			"the requested method is not supported for this resource",
		))
		return
	}

	response.WriteProblem(w, r, response.NewProblem(
		http.StatusNotFound,
		"NOT_FOUND",
		"Resource not found",
		"the requested resource does not exist",
	))
}

// bufferedWriter captures a response so the fallback can inspect its status.
type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *bufferedWriter) Header() http.Header {
	return w.header
}

func (w *bufferedWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
}

func (w *bufferedWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}
