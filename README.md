# go-event-platform

A small Go microservices showcase: an API gateway routing to an order service, which
reserves stock from an inventory service over gRPC and publishes an event that
notification and analytics services consume asynchronously over NATS. Built
incrementally as a demonstrable MVP, with room to grow into a fuller event-driven
platform (caching, observability, Kubernetes) on top of these foundations.

## Architecture

```
                 ┌──────────────┐
   client ─────► │  api-gateway │
                 │   :8080      │
                 └──────┬───────┘
                        │ REST
            ┌───────────┴────────────┐
            ▼                        ▼
   ┌─────────────────┐      ┌──────────────────────┐
   │  order-service   │ ───► │  inventory-service    │
   │   :8082          │ gRPC │   :8081 (REST reads)  │
   └────────┬─────────┘      │   :9081 (gRPC)        │
            │  publish       └───────────┬───────────┘
            │  orders.created            │
            ▼                            ▼
      ┌──────────┐                 ┌──────────────┐
      │ order-db │                 │ inventory-db │
      │ Postgres │                 │  Postgres    │
      └──────────┘                 └──────────────┘
            │
            ▼
   ┌─────────────────────┐
   │  nats (JetStream)    │
   │   :4222              │
   └──────────┬───────────┘
              │ durable consumers (independent, no HTTP between them)
     ┌────────┴─────────┐
     ▼                  ▼
┌──────────────────┐  ┌──────────────────┐
│ notification-svc   │  │ analytics-svc     │
│   :8083            │  │   :8084           │
└──────────────────┘  └──────────────────┘
```

- **api-gateway** is a thin reverse proxy — a single entry point that routes requests
  to the right backend service. No business logic of its own.
- **order-service** owns orders. Creating an order calls inventory-service over **gRPC**
  to reserve stock before persisting the order; if reservation fails, no order is
  created. After persisting, it publishes an `OrderCreated` event to NATS JetStream —
  best-effort, logged on failure but never blocking the response, since the order is
  already durable in Postgres by that point.
- **inventory-service** owns stock levels and runs two servers: its existing REST API
  (external reads, e.g. the gateway's `GET /items/{sku}`) and a **gRPC** server used only
  for the internal `ReserveStock` call from order-service. Reservation itself is an
  atomic conditional update (`quantity - N` only if enough stock is available) — the
  same `Store` backs both transports, so there's no duplicated business logic.
- **notification-service** and **analytics-service** are independent consumers of the
  `orders.created` subject via durable JetStream consumers. Neither is called directly
  by order-service, and neither knows the other exists — this is the asynchronous,
  decoupled side of the platform, in contrast to the synchronous REST calls above.
  Each uses a durable consumer, so events published while a consumer is offline are
  delivered once it reconnects (not lost, unlike plain pub/sub).
- Each service with a datastore has its **own Postgres database** — no shared schema,
  no cross-service DB access.

## Services

| Service                | Port          | Responsibility                                              | Datastore/broker |
|-------------------------|---------------|------------------------------------------------------------------|-------------------|
| `api-gateway`           | 8080          | Routes external requests to backend services                      | none              |
| `order-service`         | 8082          | Order creation/lookup; reserves stock via gRPC; publishes `OrderCreated` | `order-db`, NATS  |
| `inventory-service`     | 8081 (REST), 9081 (gRPC) | Item lookup (REST) and stock reservation (gRPC)         | `inventory-db`    |
| `notification-service`  | 8083          | Consumes `OrderCreated`, simulates sending a confirmation           | NATS              |
| `analytics-service`     | 8084          | Consumes `OrderCreated`, tracks running order/quantity totals        | NATS (in-memory stats) |

### api-gateway

| Method | Path            | Proxies to          |
|--------|-----------------|----------------------|
| GET    | `/healthz`      | (local liveness)     |
| POST   | `/orders`       | order-service        |
| GET    | `/orders/{id}`  | order-service        |
| GET    | `/items/{sku}`  | inventory-service     |

### order-service

| Method | Path            | Description                                          |
|--------|-----------------|-------------------------------------------------------|
| GET    | `/healthz`      | Liveness + DB connectivity check                       |
| POST   | `/orders`       | `{"sku": "...", "quantity": N}` → reserves stock, creates order |
| GET    | `/orders/{id}`  | Fetch an order by ID                                    |

### inventory-service

REST (port 8081, external reads):

| Method | Path            | Description                       |
|--------|-----------------|--------------------------------------|
| GET    | `/healthz`      | Liveness + DB connectivity check       |
| GET    | `/items/{sku}`  | Fetch current stock for a SKU          |

gRPC (port 9081, internal only — see `proto/inventoryv1/inventory.proto`):

| RPC            | Description                                          |
|-----------------|---------------------------------------------------------|
| `ReserveStock`  | `{sku, quantity}` → atomically decrements stock, or a gRPC status error (`NOT_FOUND`, `FAILED_PRECONDITION` for insufficient stock) |

Seed data (inserted on startup): `SKU-001` (Widget, qty 100), `SKU-002` (Gadget, qty 50).

### notification-service

| Method | Path       | Description                              |
|--------|------------|--------------------------------------------|
| GET    | `/healthz` | Liveness + NATS connection check             |

No other HTTP surface — it only consumes `orders.created` events and logs a simulated
notification (no real email/SMS integration; that's out of scope for this MVP).

### analytics-service

| Method | Path       | Description                                             |
|--------|------------|-------------------------------------------------------------|
| GET    | `/healthz` | Liveness + NATS connection check                              |
| GET    | `/stats`   | `{"orders_count": N, "total_quantity_reserved": N}` running totals |

Stats are in-memory and reset on restart; a durable store is a reasonable future upgrade.

### Event contract

order-service publishes to the `orders.created` subject on the `ORDERS` JetStream stream:

```json
{
  "order_id": "uuid",
  "sku": "SKU-001",
  "quantity": 5,
  "status": "confirmed",
  "created_at": "2026-01-01T00:00:00Z"
}
```

This is a wire contract, not shared Go code — each service (publisher and both
consumers) defines its own local copy of this struct, consistent with how services
never import each other's internal packages.

### gRPC contract

order-service and inventory-service each keep their own copy of
`proto/inventoryv1/inventory.proto` (only the generated Go package path differs) and
independently generate `internal/inventoryv1/` from it via `protoc`. As with the NATS
event above, this is a duplicated contract rather than a shared Go module — regenerate
both copies when the `.proto` changes:

```bash
protoc --proto_path=<service>/proto \
  --go_out=<service> --go_opt=module=github.com/On0n0k1/go-event-platform/<service> \
  --go-grpc_out=<service> --go-grpc_opt=module=github.com/On0n0k1/go-event-platform/<service> \
  <service>/proto/inventoryv1/inventory.proto
```

Requires `protoc` plus the `protoc-gen-go`/`protoc-gen-go-grpc` plugins on `PATH`
(`go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` and
`go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest`).

## Repo layout

This is a multi-module monorepo tied together with a Go workspace (`go.work`). Each
service is an independent Go module with its own `go.mod`/`go.sum` — they don't import
each other's code, only talk over HTTP.

```
go-event-platform/
├── go.work
├── api-gateway/            # reverse proxy
├── order-service/          # order domain + Postgres + inventory gRPC client + NATS publisher
├── inventory-service/      # inventory domain + Postgres + REST reads + gRPC reservation server
├── notification-service/   # NATS consumer, simulated notifications
├── analytics-service/      # NATS consumer, in-memory stats + /stats endpoint
├── integration/            # end-to-end tests against the real docker-compose stack
├── docker-compose.yml
└── .github/workflows/ci.yml
```

Each service follows the same internal shape (fields vary — e.g. only services with a
database have `db/`, only order/inventory-service have `proto/`+`inventoryv1/`, only
services touching NATS have `events/`):

```
<service>/
├── proto/inventoryv1/inventory.proto   # gRPC contract source (order/inventory-service only)
├── cmd/<service>/main.go   # wiring, graceful shutdown, lifecycle
├── internal/
│   ├── config/             # env-var configuration
│   ├── db/                 # Postgres pool + embedded schema (order/inventory-service only)
│   ├── events/              # NATS JetStream publisher/subscriber (order/notification/analytics)
│   ├── httpx/               # JSON helpers, logging middleware, healthz
│   ├── inventoryv1/          # generated gRPC code (order/inventory-service only)
│   └── <domain>/            # models, store, HTTP/gRPC handlers
└── Dockerfile
```

## Prerequisites

- Go 1.26+
- Docker + Docker Compose

## Running locally

The whole stack (both Postgres instances, NATS, and all five services) runs with one
command:

```bash
docker compose up --build
```

Once healthy, exercise the synchronous path through the gateway:

```bash
# check stock
curl http://localhost:8080/items/SKU-001

# place an order
curl -X POST -d '{"sku":"SKU-001","quantity":5}' http://localhost:8080/orders

# fetch the order back (use the id returned above)
curl http://localhost:8080/orders/<id>

# confirm stock was decremented
curl http://localhost:8080/items/SKU-001
```

Then check the asynchronous side picked it up (notification/analytics services aren't
routed through the gateway — hit them directly on their own ports):

```bash
# analytics-service's running totals should now include the order above
curl http://localhost:8084/stats

# notification-service logged a simulated confirmation
docker compose logs notification-service
```

Tear down (including volumes):

```bash
docker compose down -v
```

### Running a single service outside Docker

Each service reads its config from env vars (see `internal/config` in each module).
For example, inventory-service:

```bash
cd inventory-service
DATABASE_URL="postgres://inventory:inventory@localhost:55432/inventory?sslmode=disable" \
PORT=8081 \
GRPC_PORT=9081 \
go run ./cmd/inventory-service
```

(Use `docker compose up inventory-db order-db nats -d` first to get local Postgres and
NATS instances on the ports above.)

## Testing

**Unit tests** (per module, no external dependencies — Postgres/NATS/HTTP calls are
stubbed):

```bash
for m in api-gateway order-service inventory-service notification-service analytics-service; do
  (cd "$m" && go test ./...)
done
```

**Integration test** (boots the real `docker-compose.yml` stack and exercises it over
real HTTP against real Postgres — requires Docker, gated behind a build tag so it
doesn't run as part of a normal `go test`):

```bash
cd integration && go test -tags=integration -v ./...
```

**Formatting/vet check** (run from the repo root):

```bash
gofmt -l .
go vet ./api-gateway/... ./order-service/... ./inventory-service/... ./notification-service/... ./analytics-service/...
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs on every push/PR to `main`:

1. **lint** — `gofmt -l .`
2. **unit-test** — build/vet/test matrix across the five service modules
3. **integration-test** — builds and tests the full Compose stack, after the faster jobs pass

## Roadmap

This is intentionally the simplest slice that demonstrates the architecture end to end.
Planned evolution, roughly in order:

1. Add Redis caching (e.g. inventory lookups).
2. Add OpenTelemetry tracing + Prometheus metrics + Grafana dashboards.
3. Add Kubernetes manifests/Helm charts alongside the existing Compose setup.
4. Retry with exponential backoff on inter-service calls (including the order→inventory
   gRPC call, which today fails a request immediately if inventory-service is down).
5. Transactional outbox for order-service's event publish, so a NATS publish failure
   can't silently diverge from the persisted order (currently best-effort/logged only).
6. TLS for the gRPC connection between order-service and inventory-service (currently
   plaintext/insecure credentials, fine for local Compose but not production).
