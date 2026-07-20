package inventory

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/On0n0k1/go-event-platform/inventory-service/internal/httpx"
)

type Handler struct {
	store Store
	log   *slog.Logger
}

func NewHandler(store Store, log *slog.Logger) *Handler {
	return &Handler{store: store, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /items/{sku}", h.getItem)
	mux.HandleFunc("POST /items/{sku}/reserve", h.reserveStock)
}

func (h *Handler) getItem(w http.ResponseWriter, r *http.Request) {
	sku := r.PathValue("sku")

	item, err := h.store.GetItem(r.Context(), sku)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "item not found")
		return
	case err != nil:
		h.log.Error("get item failed", "sku", sku, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, item)
}

type reserveRequest struct {
	Quantity int `json:"quantity"`
}

func (h *Handler) reserveStock(w http.ResponseWriter, r *http.Request) {
	sku := r.PathValue("sku")

	var req reserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Quantity <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "quantity must be positive")
		return
	}

	item, err := h.store.ReserveStock(r.Context(), sku, req.Quantity)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "item not found")
		return
	case errors.Is(err, ErrInsufficientStock):
		httpx.WriteError(w, http.StatusConflict, "insufficient stock")
		return
	case err != nil:
		h.log.Error("reserve stock failed", "sku", sku, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, item)
}
