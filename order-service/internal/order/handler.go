package order

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/On0n0k1/go-event-platform/order-service/internal/events"
	"github.com/On0n0k1/go-event-platform/order-service/internal/httpx"
	"github.com/On0n0k1/go-event-platform/order-service/internal/inventoryclient"
)

type InventoryReserver interface {
	Reserve(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error)
}

type EventPublisher interface {
	PublishOrderCreated(ctx context.Context, evt events.OrderCreated) error
}

type Handler struct {
	store     Store
	inventory InventoryReserver
	events    EventPublisher
	log       *slog.Logger
}

func NewHandler(store Store, inventory InventoryReserver, events EventPublisher, log *slog.Logger) *Handler {
	return &Handler{store: store, inventory: inventory, events: events, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders", h.createOrder)
	mux.HandleFunc("GET /orders/{id}", h.getOrder)
}

type createOrderRequest struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SKU == "" || req.Quantity <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "sku is required and quantity must be positive")
		return
	}

	_, err := h.inventory.Reserve(r.Context(), req.SKU, req.Quantity)
	switch {
	case errors.Is(err, inventoryclient.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "item not found")
		return
	case errors.Is(err, inventoryclient.ErrInsufficientStock):
		httpx.WriteError(w, http.StatusConflict, "insufficient stock")
		return
	case err != nil:
		h.log.Error("reserve stock failed", "sku", req.SKU, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "inventory-service unavailable")
		return
	}

	o := Order{
		ID:        uuid.New().String(),
		SKU:       req.SKU,
		Quantity:  req.Quantity,
		Status:    StatusConfirmed,
		CreatedAt: time.Now().UTC(),
	}

	if err := h.store.CreateOrder(r.Context(), o); err != nil {
		h.log.Error("create order failed", "sku", req.SKU, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Best-effort: the order is already durably persisted above, so a publish
	// failure here is logged but does not fail the request. A transactional
	// outbox would close this gap; out of scope for this MVP.
	evt := events.OrderCreated{
		OrderID:   o.ID,
		SKU:       o.SKU,
		Quantity:  o.Quantity,
		Status:    o.Status,
		CreatedAt: o.CreatedAt,
	}
	if err := h.events.PublishOrderCreated(r.Context(), evt); err != nil {
		h.log.Error("publish order created event failed", "order_id", o.ID, "error", err)
	}

	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	o, err := h.store.GetOrder(r.Context(), id)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "order not found")
		return
	case err != nil:
		h.log.Error("get order failed", "id", id, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, o)
}
