package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	authapp "github.com/amirfaisalz/procure-flow/internal/application/auth"
	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/platform/requestid"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

// QueryError reports an invalid query-string parameter. It maps to a 400
// INVALID_QUERY problem, distinct from body validation (422 VALIDATION_ERROR).
type QueryError struct {
	Field   string
	Message string
}

func (e *QueryError) Error() string {
	return "invalid query parameter " + e.Field + ": " + e.Message
}

// WriteError translates application and domain errors into RFC 9457 problems.
// Unknown errors are logged and reported as a generic 500 so internal details
// never leak to clients.
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	if err == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	var queryErr *QueryError
	if errors.As(err, &queryErr) {
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusBadRequest,
			"INVALID_QUERY",
			"Invalid query parameter",
			queryErr.Error(),
		))
		return
	}

	var decodeErr *response.DecodeError
	if errors.As(err, &decodeErr) {
		response.WriteProblem(w, r, response.NewProblem(
			decodeErr.Status,
			decodeErr.Code,
			http.StatusText(decodeErr.Status),
			decodeErr.Message,
		))
		return
	}

	var validationErr *domain.ValidationError
	if errors.As(err, &validationErr) {
		problem := response.NewProblem(
			http.StatusUnprocessableEntity,
			"VALIDATION_ERROR",
			"Validation failed",
			"one or more fields are invalid",
		)
		problem.Errors = validationErr.Fields
		response.WriteProblem(w, r, problem)
		return
	}

	switch {
	case errors.Is(err, domain.ErrNotFound):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusNotFound,
			"NOT_FOUND",
			"Resource not found",
			"the requested resource does not exist",
		))
		return
	case errors.Is(err, domain.ErrForbidden):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusForbidden,
			"FORBIDDEN",
			"Forbidden",
			"you are not allowed to perform this action",
		))
		return
	case errors.Is(err, domain.ErrInvalidTransition):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusConflict,
			"INVALID_TRANSITION",
			"Invalid status transition",
			"the requested status transition is not allowed",
		))
		return
	case errors.Is(err, authapp.ErrInvalidCredentials):
		w.Header().Set("WWW-Authenticate", `Bearer realm="procure-flow"`)
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusUnauthorized,
			"INVALID_CREDENTIALS",
			"Invalid credentials",
			"the email or password is incorrect",
		))
		return
	case errors.Is(err, domain.ErrConflict):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusConflict,
			"CONFLICT",
			"Resource conflict",
			"the resource was modified by another request",
		))
		return
	case errors.Is(err, context.DeadlineExceeded):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusGatewayTimeout,
			"TIMEOUT",
			"Request timeout",
			"the request took too long to complete",
		))
		return
	case errors.Is(err, context.Canceled):
		response.WriteProblem(w, r, response.NewProblem(
			http.StatusRequestTimeout,
			"REQUEST_CANCELED",
			"Request canceled",
			"the request was canceled by the client",
		))
		return
	}

	logger.ErrorContext(r.Context(), "unhandled error",
		slog.String("error", err.Error()),
		slog.String("request_id", requestid.From(r.Context())),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
	response.WriteProblem(w, r, response.NewProblem(
		http.StatusInternalServerError,
		"INTERNAL_ERROR",
		"Internal server error",
		"an unexpected error occurred",
	))
}
