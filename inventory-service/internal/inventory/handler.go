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
	mux.HandleFunc("POST /items/{sku}/restock", h.restockItem)
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

type restockRequest struct {
	Quantity int `json:"quantity"`
}

func (h *Handler) restockItem(w http.ResponseWriter, r *http.Request) {
	sku := r.PathValue("sku")

	var req restockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Quantity <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "quantity must be positive")
		return
	}

	item, err := h.store.RestockItem(r.Context(), sku, req.Quantity)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "item not found")
		return
	case err != nil:
		h.log.Error("restock item failed", "sku", sku, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	itemsRestockedTotal.Inc()
	httpx.WriteJSON(w, http.StatusOK, item)
}
