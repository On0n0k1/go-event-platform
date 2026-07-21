# go-event-platform

A small Go microservices showcase: an API gateway routing to an order service, which
reserves stock from an inventory service. Built incrementally as a demonstrable MVP,
with room to grow into a fuller event-driven platform (async messaging, gRPC, caching,
observability, Kubernetes) on top of these foundations.

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
   │   :8082          │ REST │   :8081                │
   └────────┬─────────┘      └───────────┬───────────┘
            │                            │
            ▼                            ▼
      ┌──────────┐                ┌──────────────┐
      │ order-db │                │ inventory-db │
      │ Postgres │                │  Postgres    │
      └──────────┘                └──────────────┘
```

- **api-gateway** is a thin reverse proxy — a single entry point that routes requests
  to the right backend service. No business logic of its own.
- **order-service** owns orders. Creating an order synchronously calls inventory-service
  to reserve stock before persisting the order; if reservation fails, no order is created.
- **inventory-service** owns stock levels. Reservation is an atomic conditional update
  (`quantity - N` only if enough stock is available).
- Each service has its **own Postgres database** — no shared schema, no cross-service
  DB access. Services only ever talk to each other over HTTP.

## Services

| Service             | Port | Responsibility                                  | Datastore     |
|----------------------|------|--------------------------------------------------|---------------|
| `api-gateway`        | 8080 | Routes external requests to backend services      | none          |
| `order-service`      | 8082 | Order creation/lookup; reserves stock on create   | `order-db`    |
| `inventory-service`  | 8081 | Item lookup and stock reservation                 | `inventory-db`|

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

| Method | Path                     | Description                                  |
|--------|--------------------------|------------------------------------------------|
| GET    | `/healthz`               | Liveness + DB connectivity check                 |
| GET    | `/items/{sku}`           | Fetch current stock for a SKU                    |
| POST   | `/items/{sku}/reserve`   | `{"quantity": N}` → atomically decrements stock  |

Seed data (inserted on startup): `SKU-001` (Widget, qty 100), `SKU-002` (Gadget, qty 50).

## Repo layout

This is a multi-module monorepo tied together with a Go workspace (`go.work`). Each
service is an independent Go module with its own `go.mod`/`go.sum` — they don't import
each other's code, only talk over HTTP.

```
go-event-platform/
├── go.work
├── api-gateway/          # reverse proxy
├── order-service/        # order domain + Postgres + inventory HTTP client
├── inventory-service/    # inventory domain + Postgres
├── integration/          # end-to-end tests against the real docker-compose stack
├── docker-compose.yml
└── .github/workflows/ci.yml
```

Each service follows the same internal shape:

```
<service>/
├── cmd/<service>/main.go   # wiring, graceful shutdown, lifecycle
├── internal/
│   ├── config/             # env-var configuration
│   ├── db/                 # Postgres pool + embedded schema
│   ├── httpx/               # JSON helpers, logging middleware, healthz
│   └── <domain>/            # models, store, HTTP handlers
└── Dockerfile
```

## Prerequisites

- Go 1.26+
- Docker + Docker Compose

## Running locally

The whole stack (both Postgres instances + all three services) runs with one command:

```bash
docker compose up --build
```

Once healthy, exercise it through the gateway:

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
go run ./cmd/inventory-service
```

(Use `docker compose up inventory-db order-db -d` first to get local Postgres instances
on the ports above.)

## Testing

**Unit tests** (per module, no external dependencies — Postgres/HTTP calls are stubbed):

```bash
cd inventory-service && go test ./...
cd order-service && go test ./...
cd api-gateway && go test ./...
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
go vet ./api-gateway/... ./order-service/... ./inventory-service/...
```

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs on every push/PR to `main`:

1. **lint** — `gofmt -l .`
2. **unit-test** — build/vet/test matrix across the three service modules
3. **integration-test** — builds and tests the full Compose stack, after the faster jobs pass

## Roadmap

This is intentionally the simplest slice that demonstrates the architecture end to end.
Planned evolution, roughly in order:

1. Add NATS + a `notification-service`/`analytics-service` consuming an `OrderCreated`
   event — introduces the asynchronous/event-driven side of the platform.
2. Swap the order→inventory HTTP call for gRPC.
3. Add Redis caching (e.g. inventory lookups).
4. Add OpenTelemetry tracing + Prometheus metrics + Grafana dashboards.
5. Add Kubernetes manifests/Helm charts alongside the existing Compose setup.
6. Retry with exponential backoff on inter-service calls.
