package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/transport/httpapi/response"
)

type createRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Department  string `json:"department"`
	Requester   string `json:"requester"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

type procurementResponse struct {
	ID              string    `json:"id"`
	Number          string    `json:"number"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Department      string    `json:"department"`
	Requester       string    `json:"requester"`
	RequesterID     string    `json:"requester_id,omitempty"`
	Amount          int64     `json:"amount"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Version         int       `json:"version"`
}

type auditEventResponse struct {
	ID         int64             `json:"id"`
	Action     string            `json:"action"`
	ActorID    string            `json:"actor_id"`
	ActorName  string            `json:"actor_name"`
	ActorRole  string            `json:"actor_role"`
	FromStatus string            `json:"from_status,omitempty"`
	ToStatus   string            `json:"to_status,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	OccurredAt time.Time         `json:"occurred_at"`
}

type pageMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// Procurement exposes the procurement use cases over HTTP.
type Procurement struct {
	service *app.Service
	logger  *slog.Logger
}

// NewProcurement creates the procurement HTTP handler.
func NewProcurement(service *app.Service, logger *slog.Logger) *Procurement {
	if logger == nil {
		logger = slog.Default()
	}
	return &Procurement{service: service, logger: logger}
}

// Create handles POST /api/v1/procurements.
func (h *Procurement) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := response.DecodeJSON(w, r, &req); err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	created, err := h.service.Create(r.Context(), app.CreateInput{
		Title:       req.Title,
		Description: req.Description,
		Department:  req.Department,
		Requester:   req.Requester,
		Amount:      req.Amount,
		Currency:    req.Currency,
	})
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/api/v1/procurements/"+created.ID)
	response.JSON(w, http.StatusCreated, toResponse(created))
}

// List handles GET /api/v1/procurements.
func (h *Procurement) List(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListFilter(r)
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	result, err := h.service.List(r.Context(), filter)
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	items := make([]procurementResponse, len(result.Items))
	for i, item := range result.Items {
		items[i] = toResponse(item)
	}

	response.JSONWithMeta(w, http.StatusOK, items, pageMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		Total:      result.Total,
		TotalPages: result.TotalPages(),
	})
}

// Get handles GET /api/v1/procurements/{id}.
func (h *Procurement) Get(w http.ResponseWriter, r *http.Request) {
	got, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}
	response.JSON(w, http.StatusOK, toResponse(got))
}

// Audit handles GET /api/v1/procurements/{id}/audit.
func (h *Procurement) Audit(w http.ResponseWriter, r *http.Request) {
	events, err := h.service.Audit(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	items := make([]auditEventResponse, len(events))
	for i, event := range events {
		items[i] = auditEventResponse{
			ID:         event.ID,
			Action:     event.Action,
			ActorID:    event.ActorID,
			ActorName:  event.ActorName,
			ActorRole:  event.ActorRole,
			FromStatus: event.FromStatus,
			ToStatus:   event.ToStatus,
			Metadata:   event.Metadata,
			OccurredAt: event.OccurredAt,
		}
	}
	response.JSON(w, http.StatusOK, items)
}

// Submit handles POST /api/v1/procurements/{id}/submit.
func (h *Procurement) Submit(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func() (procurement.Procurement, error) {
		return h.service.Submit(r.Context(), r.PathValue("id"))
	})
}

// Approve handles POST /api/v1/procurements/{id}/approve.
func (h *Procurement) Approve(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func() (procurement.Procurement, error) {
		return h.service.Approve(r.Context(), r.PathValue("id"))
	})
}

// Cancel handles POST /api/v1/procurements/{id}/cancel.
func (h *Procurement) Cancel(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func() (procurement.Procurement, error) {
		return h.service.Cancel(r.Context(), r.PathValue("id"))
	})
}

// Reject handles POST /api/v1/procurements/{id}/reject.
func (h *Procurement) Reject(w http.ResponseWriter, r *http.Request) {
	var req rejectRequest
	if err := response.DecodeJSON(w, r, &req); err != nil {
		WriteError(w, r, h.logger, err)
		return
	}

	h.transition(w, r, func() (procurement.Procurement, error) {
		return h.service.Reject(r.Context(), r.PathValue("id"), req.Reason)
	})
}

func (h *Procurement) transition(
	w http.ResponseWriter,
	r *http.Request,
	apply func() (procurement.Procurement, error),
) {
	updated, err := apply()
	if err != nil {
		WriteError(w, r, h.logger, err)
		return
	}
	response.JSON(w, http.StatusOK, toResponse(updated))
}

func parseListFilter(r *http.Request) (procurement.ListFilter, error) {
	query := r.URL.Query()
	filter := procurement.ListFilter{Query: query.Get("q")}

	if raw := strings.TrimSpace(query.Get("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || page < 1 {
			return procurement.ListFilter{}, &QueryError{Field: "page", Message: "must be a positive integer"}
		}
		filter.Page = page
	}

	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		pageSize, err := strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > procurement.MaxPageSize {
			return procurement.ListFilter{}, &QueryError{
				Field:   "page_size",
				Message: "must be between 1 and " + strconv.Itoa(procurement.MaxPageSize),
			}
		}
		filter.PageSize = pageSize
	}

	if raw := strings.TrimSpace(query.Get("status")); raw != "" {
		status := procurement.Status(raw)
		if !status.Valid() {
			return procurement.ListFilter{}, &QueryError{Field: "status", Message: "unknown status"}
		}
		filter.Status = status
	}

	return filter, nil
}

func toResponse(p procurement.Procurement) procurementResponse {
	return procurementResponse{
		ID:              p.ID,
		Number:          p.Number,
		Title:           p.Title,
		Description:     p.Description,
		Department:      p.Department,
		Requester:       p.Requester,
		RequesterID:     p.RequesterID,
		Amount:          p.Amount,
		Currency:        p.Currency,
		Status:          string(p.Status),
		RejectionReason: p.RejectionReason,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
		Version:         p.Version,
	}
}
