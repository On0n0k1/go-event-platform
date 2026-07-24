package order

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/On0n0k1/go-event-platform/order-service/internal/events"
	"github.com/On0n0k1/go-event-platform/order-service/internal/httpx"
	"github.com/On0n0k1/go-event-platform/order-service/internal/inventoryclient"
)

type InventoryReserver interface {
	Reserve(ctx context.Context, sku string, quantity int) (inventoryclient.Item, error)
}

type Handler struct {
	store     Store
	inventory InventoryReserver
	log       *slog.Logger
}

func NewHandler(store Store, inventory InventoryReserver, log *slog.Logger) *Handler {
	return &Handler{store: store, inventory: inventory, log: log}
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
		stockReservationFailuresTotal.WithLabelValues("not_found").Inc()
		httpx.WriteError(w, http.StatusNotFound, "item not found")
		return
	case errors.Is(err, inventoryclient.ErrInsufficientStock):
		stockReservationFailuresTotal.WithLabelValues("insufficient_stock").Inc()
		httpx.WriteError(w, http.StatusConflict, "insufficient stock")
		return
	case err != nil:
		stockReservationFailuresTotal.WithLabelValues("unavailable").Inc()
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

	evtPayload, err := json.Marshal(events.OrderCreated{
		OrderID:   o.ID,
		SKU:       o.SKU,
		Quantity:  o.Quantity,
		Status:    o.Status,
		CreatedAt: o.CreatedAt,
	})
	if err != nil {
		h.log.Error("encode order created event failed", "sku", req.SKU, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	outboxEvt := OutboxEvent{
		Subject:     events.SubjectCreated,
		Payload:     evtPayload,
		TraceParent: traceParent(r.Context()),
	}

	// The order and its outbox event commit in one transaction: either both
	// are durably recorded or neither is. A background relay (internal/outbox)
	// publishes the event afterward, so a NATS outage never risks an order
	// existing with no event ever queued for it.
	if err := h.store.CreateOrder(r.Context(), o, outboxEvt); err != nil {
		h.log.Error("create order failed", "sku", req.SKU, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	ordersCreatedTotal.Inc()

	httpx.WriteJSON(w, http.StatusCreated, o)
}

// traceParent extracts the W3C traceparent header for the current span, so
// it can be stored alongside the outbox event and restored by the relay when
// it eventually publishes -- keeping the event on the same trace as the
// request that created it, even though publishing happens later.
func traceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
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
