# go-event-platform

A small Go microservices showcase: an API gateway routing to an order service, which
reserves stock from a Redis-cached inventory service over mutually-authenticated TLS
gRPC and publishes an event that notification and analytics services consume
asynchronously over NATS — all traced end to end with OpenTelemetry into Jaeger, and
all exposing Prometheus metrics on a provisioned Grafana dashboard. Built incrementally
as a demonstrable MVP, with room to grow into a fuller event-driven platform (Kubernetes
and beyond) on top of these foundations.

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
   │  order-service   │ ───► │  inventory-service    │──► redis (cache-aside,
   │   :8082          │ gRPC │   :8081 (REST reads)  │    GetItem only)
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

  (all five services also export OTLP traces to jaeger :4317, UI on :16686,
   and expose /metrics for prometheus :9090, visualized in grafana :3000)
```

- **api-gateway** is a thin reverse proxy — a single entry point that routes requests
  to the right backend service. No business logic of its own.
- **order-service** owns orders. Creating an order calls inventory-service over **gRPC**
  to reserve stock before persisting the order; if reservation fails, no order is
  created. That call retries with backoff on connection-level failures (see
  [Retry behavior](#retry-behavior)) rather than failing the request on the first blip.
  The order and its `OrderCreated` event are then written **atomically** in one Postgres
  transaction via a transactional outbox (see [Transactional outbox](#transactional-outbox))
  — a background relay publishes it to NATS afterward, so a NATS outage can never cause
  an order to exist with no event ever queued for it.
- **inventory-service** owns stock levels and runs two servers: its existing REST API
  (external reads, e.g. the gateway's `GET /items/{sku}`) and a **gRPC** server, secured
  with **mutual TLS**, used only for the internal `ReserveStock` call from order-service
  (see [gRPC mTLS](#grpc-mtls)). Reservation itself is an atomic conditional update
  (`quantity - N` only if enough stock is available) — the
  same `Store` backs both transports, so there's no duplicated business logic. Reads go
  through a Redis cache-aside layer (`CachingStore`, 30s TTL); a successful reservation
  always invalidates that SKU's cache entry rather than trusting its own write, so a
  read right after a reservation is never served stale data — any Redis error is treated
  as a cache miss and falls back to Postgres, so a Redis outage degrades performance,
  not correctness.
- **notification-service** and **analytics-service** are independent consumers of the
  `orders.created` subject via durable JetStream consumers. Neither is called directly
  by order-service, and neither knows the other exists — this is the asynchronous,
  decoupled side of the platform, in contrast to the synchronous REST calls above.
  Each uses a durable consumer, so events published while a consumer is offline are
  delivered once it reconnects (not lost, unlike plain pub/sub).
- Each service with a datastore has its **own Postgres database** — no shared schema,
  no cross-service DB access.
- Every service is instrumented with **OpenTelemetry**, exporting to a **Jaeger**
  container. Trace context propagates automatically across REST (via `otelhttp`, both
  server and — for the gateway's proxy — client side) and gRPC (via `otelgrpc`), and by
  hand across the NATS hop (order-service injects trace context into message headers on
  publish; notification-service/analytics-service extract it on consume), so a single
  order produces one trace spanning all five services, including the async fan-out to
  both NATS consumers. Every request/event log line also carries the same `trace_id`,
  so logs and traces correlate directly.
- Every service exposes `GET /metrics` for **Prometheus** to scrape (`prometheus/client_golang`,
  not OTel's metrics SDK — a lighter, more common pairing than routing metrics through OTel
  too). HTTP and gRPC request count/duration are recorded automatically; a few domain
  metrics are recorded by hand where they say something the transport-level ones can't
  (`orders_created_total`, `stock_reservation_failures_total{reason}`,
  `inventory_item_cache_result_total{result}`, `notifications_sent_total`,
  `analytics_orders_recorded_total`). A provisioned **Grafana** dashboard (Prometheus +
  Jaeger datasources, both checked into the repo) is available immediately on startup —
  no manual clicking required.

## Services

| Service                | Port          | Responsibility                                              | Datastore/broker |
|-------------------------|---------------|------------------------------------------------------------------|-------------------|
| `api-gateway`           | 8080          | Routes external requests to backend services                      | none              |
| `order-service`         | 8082          | Order creation/lookup; reserves stock via gRPC; publishes `OrderCreated` | `order-db`, NATS  |
| `inventory-service`     | 8081 (REST), 9081 (gRPC) | Item lookup (REST, cached) and stock reservation (gRPC) | `inventory-db`, Redis |
| `notification-service`  | 8083          | Consumes `OrderCreated`, simulates sending a confirmation           | NATS              |
| `analytics-service`     | 8084          | Consumes `OrderCreated`, tracks running order/quantity totals        | NATS (in-memory stats) |

### api-gateway

| Method | Path            | Proxies to          |
|--------|-----------------|----------------------|
| GET    | `/`             | (embedded static UI -- see below) |
| GET    | `/healthz`      | (local liveness)     |
| GET    | `/metrics`      | (local Prometheus metrics) |
| POST   | `/orders`       | order-service        |
| GET    | `/orders/{id}`  | order-service        |
| GET    | `/items/{sku}`  | inventory-service     |
| POST   | `/items/{sku}/restock` | inventory-service |
| GET    | `/stats`        | analytics-service     |

`GET /` serves a minimal, dependency-free HTML/JS page (`api-gateway/cmd/api-gateway/web/`,
embedded into the binary via `go:embed`) for checking stock, placing orders, looking
orders back up, and watching analytics-service's live stats -- the same operations shown
in [Running locally](#running-locally) below, just clickable instead of curl. It's
same-origin with the API it calls, so no CORS setup is needed; open
http://localhost:8080 (compose) or http://localhost:8090 (Kubernetes Ingress) in a
browser.

### order-service

| Method | Path            | Description                                          |
|--------|-----------------|-------------------------------------------------------|
| GET    | `/healthz`      | Liveness + DB connectivity check                       |
| GET    | `/metrics`      | Prometheus metrics                                      |
| POST   | `/orders`       | `{"sku": "...", "quantity": N}` → reserves stock, creates order |
| GET    | `/orders/{id}`  | Fetch an order by ID                                    |

### inventory-service

REST (port 8081, external reads):

| Method | Path            | Description                       |
|--------|-----------------|--------------------------------------|
| GET    | `/healthz`      | Liveness + DB connectivity check       |
| GET    | `/metrics`      | Prometheus metrics                      |
| GET    | `/items/{sku}`  | Fetch current stock for a SKU (Redis cache-aside, 30s TTL) |
| POST   | `/items/{sku}/restock` | `{quantity}` (must be positive) → atomically increments stock, invalidating the cache entry same as a reservation |

gRPC (port 9081, internal only — see `proto/inventoryv1/inventory.proto`):

| RPC            | Description                                          |
|-----------------|---------------------------------------------------------|
| `ReserveStock`  | `{sku, quantity}` → atomically decrements stock, or a gRPC status error (`NOT_FOUND`, `FAILED_PRECONDITION` for insufficient stock) |

Seed data (inserted on startup): `SKU-001` (Widget, qty 100), `SKU-002` (Gadget, qty 50).

### notification-service

| Method | Path       | Description                              |
|--------|------------|--------------------------------------------|
| GET    | `/healthz` | Liveness + NATS connection check             |
| GET    | `/metrics` | Prometheus metrics                             |

No other HTTP surface — it only consumes `orders.created` events and logs a simulated
notification (no real email/SMS integration; that's out of scope for this MVP).

### analytics-service

| Method | Path       | Description                                             |
|--------|------------|-------------------------------------------------------------|
| GET    | `/healthz` | Liveness + NATS connection check                              |
| GET    | `/metrics` | Prometheus metrics                                             |
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

### gRPC mTLS

The order-service → inventory-service gRPC connection uses **mutual TLS**: inventory-service
requires and verifies a client certificate on every connection
(`tls.RequireAndVerifyClientCert`), not just encryption. A `certs` service in
docker-compose.yml runs `certs/generate.sh` once before either service starts
(`depends_on: condition: service_completed_successfully`), generating a throwaway
self-signed CA plus a server cert (`CN=inventory-service`) and a client cert
(`CN=order-service`) into a host-mounted `./certs/` directory — `docker compose up`
needs no manual setup step, and the same directory is readable outside Docker too (see
below). Each service loads its identity via a small `internal/tlsconfig` package
(`Server`/`Client`, one function each — they only differ in `ClientAuth`/`RootCAs`, not
worth sharing across modules for that).

`certs/` is gitignored (`generate.sh` itself is checked in); regenerate at any time by
deleting the directory's contents and re-running `docker compose up certs`, or the full
stack.

Verified live: `openssl s_client` against inventory-service's gRPC port without a client
cert fails the handshake with `tlsv13 alert certificate required`; with the generated
`order-service.crt`/`.key`, it succeeds — confirming the server genuinely enforces mTLS
rather than just having unused code paths for it.

### Retry behavior

order-service's `inventoryclient` (`internal/retry`, a small generic exponential-backoff
helper with equal jitter) retries the `ReserveStock` call, but **only** on
`codes.Unavailable` — connection-level failures where the request never reached the
server. `ReserveStock` mutates state and isn't idempotent, so anything else (including
`DeadlineExceeded`, where the server may already have processed the request) is left
alone rather than risking a double reservation.

Two things had to be tuned together to make this safe and actually useful:

- **The retry budget** (`reserveRetryConfig`: 6 attempts, 300ms base / 2s max delay) is
  deliberately sized to plausibly bridge a real restart of inventory-service (a redeploy,
  a crash-restart), not just a sub-second blip — a conscious latency-for-resilience
  trade-off on a synchronous, user-facing request.
- **`grpc.WithConnectParams(MinConnectTimeout: 2s)`** on the client bounds how long a
  single dial attempt can take. Without it, an unreachable-but-not-actively-refusing peer
  (e.g. a stopped container whose IP briefly still resolves) hangs on the OS's own TCP
  connect timeout — observed at 25s in testing — which swallows the retry budget entirely
  regardless of how it's configured. With it, a bad attempt fails fast enough for the
  retry loop to actually do its job.

Verified live: killing inventory-service and restarting it mid-request lets a
`POST /orders` succeed transparently (no client-visible error, ~3-4s added latency,
visible as `retry` events on the request's trace span and in `grpc_client_retries_total`)
as long as it comes back within the budget; if it doesn't, the request still fails
cleanly with a 502 in a bounded ~5-6s rather than hanging.

### Transactional outbox

Publishing `OrderCreated` used to happen inline in the HTTP handler, right after the
order was saved — two independent writes (Postgres, then NATS) with no atomicity between
them. If the NATS publish failed, the order still existed but the event that was
supposed to announce it was gone for good: the classic **dual-write problem**.

Now `order.PostgresStore.CreateOrder` inserts the order **and** an `outbox_events` row
in the same DB transaction — both commit together or neither does. A background relay
(`internal/outbox`, started as a goroutine in `main.go`) polls for unpublished rows every
500ms, publishes each to NATS, and marks it published; a failed publish just leaves the
row for the next poll to retry. The order itself no longer depends on NATS being up at
all.

This introduced a real bug worth calling out: the relay's retries are **at-least-once**
against an ambiguous failure mode (a timeout doesn't tell you whether the server actually
stored the message before the ack was lost), so a naive retry-until-success loop can
publish the same logical event to the stream more than once. Live testing surfaced
exactly this — one order was delivered to both consumers 5 times after a NATS outage.
The fix is JetStream's built-in dedup: `Publish` sets a stable `Nats-Msg-Id` header
(`outbox-<row id>`), and the server silently drops duplicates within its default 2-minute
window, making the retries safe regardless of how many times they fire.

The relay also restores the trace context that was captured (as a `traceparent` column)
in the same transaction as the order, so the event it publishes — possibly seconds later,
on a totally different goroutine — still continues that original request's trace rather
than starting a disconnected one; see [Tracing](#tracing).

Verified live: creating an order while NATS is fully stopped still returns `201`; the
event sits unpublished in `outbox_events` until NATS comes back, at which point the relay
delivers it — exactly once, confirmed via both consumers' logs and Prometheus-independent
inspection of the table — with no client-visible sign anything was ever wrong.

### Tracing

Each service has an `internal/tracing` package (identical across services, like
`httpx` — plain infrastructure, not a wire contract, so it's duplicated rather than
shared) that sets up a global OTel `TracerProvider` exporting via OTLP/gRPC to Jaeger,
and registers the W3C `traceparent` propagator. From there:

- **REST**: each service's HTTP handler is wrapped with `otelhttp.NewHandler` (server
  spans), filtered to skip `/healthz` so healthchecks don't spam every trace. api-gateway
  additionally instruments its reverse proxy's outbound `Transport` with
  `otelhttp.NewTransport`, so the span it creates as a server is what actually gets
  propagated downstream — not just whatever headers the original client happened to send.
- **gRPC**: order-service's client and inventory-service's server both use `otelgrpc`
  stats handlers, which propagate context automatically over gRPC metadata.
- **NATS**: no official OTel instrumentation exists for `nats.go`, so this is manual —
  order-service injects trace context into NATS message headers on publish
  (`natsHeaderCarrier`, an adapter over `nats.Header`), and notification-service /
  analytics-service extract it on consume before starting their own span. This is what
  keeps a single order's trace connected across the async fan-out to both consumers.

View traces at http://localhost:16686 after placing an order — a single trace should
show `api-gateway → order-service → inventory-service` (gRPC) plus
`order-service → notification-service` and `order-service → analytics-service` (NATS),
all under one trace ID.

### Metrics and dashboards

Each service has an `internal/metrics` package (duplicated, like `tracing/`) built on
`prometheus/client_golang`:

- `HTTPMiddleware` records `http_requests_total` / `http_request_duration_seconds`,
  labeled by method, route pattern, and status. It reads `r.Pattern` (set by `net/http`'s
  `ServeMux` once routing completes, e.g. `GET /items/{sku}`) rather than the raw URL, so
  parameterized routes don't create unbounded label cardinality.
- order-service and inventory-service additionally have hand-rolled gRPC unary
  interceptors (`grpc_client_requests_total` / `grpc_server_requests_total`, both with
  duration histograms) — `grpc-ecosystem`'s Prometheus middleware pulled in an
  incompatible old `genproto`, so this was simpler to write directly than to fight.
- `/metrics` is registered like any other route, then excluded from the same
  `otelhttp` filter that already skips `/healthz`, so scrapes don't spam every trace.

A `prometheus` container scrapes all five services' `/metrics` every 5s
(`observability/prometheus/prometheus.yml`), and a `grafana` container comes up with
Prometheus and Jaeger already added as datasources and a dashboard already loaded
(`observability/grafana/`, provisioned via mounted volumes — nothing to click through
manually). Open http://localhost:3000 (anonymous access enabled, no login) for request
rate/error-rate/p95-latency per service, gRPC request rate, orders-created rate, stock
reservation failures by reason, the inventory cache hit ratio, and restock rate
(`items_restocked_total`).

## Repo layout

This is a multi-module monorepo tied together with a Go workspace (`go.work`). Each
service is an independent Go module with its own `go.mod`/`go.sum` — they don't import
each other's code, only talk over HTTP.

```
go-event-platform/
├── go.work
├── api-gateway/            # reverse proxy
├── order-service/          # order domain + Postgres + inventory gRPC client + NATS publisher
├── inventory-service/      # inventory domain + Postgres + Redis cache + REST reads + gRPC reservation server
├── notification-service/   # NATS consumer, simulated notifications
├── analytics-service/      # NATS consumer, in-memory stats + /stats endpoint
├── integration/            # end-to-end tests against the real docker-compose stack
├── observability/          # prometheus.yml + grafana provisioning (datasources, dashboard)
├── certs/                  # generate.sh (checked in) + generated dev mTLS certs (gitignored)
├── k8s/                    # Kubernetes manifests (see "Running on Kubernetes" below)
├── helm/go-event-platform/ # Helm packaging of the same manifests (see "Packaging as Helm" below)
├── kind-config.yaml        # kind cluster config (Ingress host port mapping)
├── docker-compose.yml
└── .github/workflows/ci.yml
```

Each service follows the same internal shape (fields vary — e.g. only services with a
database have `db/`, only order/inventory-service have `proto/`+`inventoryv1/`+`tlsconfig/`,
only inventory-service has `cache/`, only order-service has `retry/`+`outbox/`, only
services touching NATS have `events/`; `tracing/` and `metrics/` are the two packages
every service has):

```
<service>/
├── proto/inventoryv1/inventory.proto   # gRPC contract source (order/inventory-service only)
├── cmd/<service>/main.go   # wiring, graceful shutdown, lifecycle
├── cmd/api-gateway/web/    # embedded static UI (api-gateway only, see "api-gateway" above)
├── internal/
│   ├── cache/               # Redis client helper (inventory-service only)
│   ├── config/             # env-var configuration
│   ├── db/                 # Postgres pool + embedded schema (order/inventory-service only)
│   ├── events/              # NATS JetStream publisher/subscriber (order/notification/analytics)
│   ├── httpx/               # JSON helpers, logging middleware (+ trace_id), healthz
│   ├── inventoryv1/          # generated gRPC code (order/inventory-service only)
│   ├── metrics/              # Prometheus HTTP middleware (+ gRPC interceptors on order/inventory-service)
│   ├── outbox/                # transactional outbox relay (order-service only)
│   ├── retry/                 # generic exponential-backoff helper (order-service only)
│   ├── tlsconfig/             # mTLS cert/key loading (order/inventory-service only)
│   ├── tracing/              # OTel TracerProvider setup/shutdown (every service)
│   └── <domain>/            # models, store (+ CachingStore decorator), HTTP/gRPC handlers
└── Dockerfile
```

## Prerequisites

- Go 1.26+
- Docker + Docker Compose
- kubectl and [kind](https://kind.sigs.k8s.io/) (only needed for [Running on Kubernetes](#running-on-kubernetes))

## Running locally

The whole stack (both Postgres instances, NATS, Redis, Jaeger, Prometheus, Grafana, and
all five services) runs with one command — this also generates the dev mTLS certs into
`./certs/` on first run (see [gRPC mTLS](#grpc-mtls)), nothing manual required:

```bash
docker compose up --build
```

Once healthy, open http://localhost:8080 for a minimal browser UI (check stock, place
orders, watch stats update live), or exercise the same synchronous path through the
gateway with curl:

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

Check the cache directly if you want to see it working — the key should exist with a TTL
after the read above, and be gone immediately after a reservation:

```bash
docker compose exec redis redis-cli GET item:SKU-001
docker compose exec redis redis-cli TTL item:SKU-001
```

Check the outbox directly if you want to see it working — the row should flip to
published within ~500ms of the order above:

```bash
docker compose exec order-db psql -U orders -d orders \
  -c "SELECT id, subject, published_at IS NOT NULL AS published FROM outbox_events ORDER BY id DESC LIMIT 5;"
```

Then check the asynchronous side picked it up (notification/analytics services aren't
routed through the gateway — hit them directly on their own ports):

```bash
# analytics-service's running totals should now include the order above
curl http://localhost:8084/stats

# notification-service logged a simulated confirmation
docker compose logs notification-service
```

Then open http://localhost:16686 (Jaeger UI), pick any of the five services, and find
the trace for the order above — it should show the full path through every service,
including the async hop to notification-service and analytics-service.

Or open http://localhost:3000 (Grafana, no login needed) for the "go-event-platform
overview" dashboard — place a few more orders and watch the request rate / latency /
orders-created panels move.

Tear down (including volumes):

```bash
docker compose down -v
```

### Running a single service outside Docker

Each service reads its config from env vars (see `internal/config` in each module).
inventory-service and order-service need `./certs/` populated first (`docker compose up
certs`, or the full stack once — it's a host directory, so it's readable outside Docker
too). For example, inventory-service:

```bash
cd inventory-service
DATABASE_URL="postgres://inventory:inventory@localhost:55432/inventory?sslmode=disable" \
PORT=8081 \
GRPC_PORT=9081 \
REDIS_ADDR=localhost:6379 \
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
TLS_CERT_FILE=../certs/inventory-service.crt \
TLS_KEY_FILE=../certs/inventory-service.key \
TLS_CA_FILE=../certs/ca.crt \
go run ./cmd/inventory-service
```

(Use `docker compose up inventory-db order-db nats redis jaeger -d` first to get local
Postgres, NATS, Redis, and Jaeger instances on the ports above.)

## Running on Kubernetes

The manifests in `k8s/` reproduce the same stack as `docker-compose.yml` — same
services, same env vars, same mTLS setup — targeting a local [kind](https://kind.sigs.k8s.io/)
cluster. There's no registry involved: images are built locally and loaded straight into
the cluster's node.

`k8s/run.sh` wraps cluster setup (kind, ingress-nginx, image build/load, TLS secrets)
plus deploying via the [Helm chart](#packaging-as-helm) into two commands:

```bash
k8s/run.sh up      # create the cluster and deploy everything
k8s/run.sh down    # uninstall the release and delete the cluster
```

It's idempotent — rerunning `up` reuses an existing cluster and does `helm upgrade
--install` rather than failing. Everything up through TLS secrets below is exactly what
it runs, spelled out for when you want to do a piece by hand (e.g. after changing one
manifest) or use the raw `kubectl apply -f k8s/` path instead of Helm, as the last step
shows.

Create the cluster and namespace. `kind-config.yaml` maps host ports 8090/8443 to the
node (adjust if those are free/taken differently on your machine — 80/443 are the kind
defaults, but are often already bound locally) and labels the node `ingress-ready` so
ingress-nginx schedules onto it:

```bash
kind create cluster --config kind-config.yaml
kubectl apply -f k8s/00-namespace.yaml
```

Install [ingress-nginx](https://kubernetes.github.io/ingress-nginx/) (its kind-specific
manifest, which wires up the hostPort mapping above) and wait for its controller pod:

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller --timeout=120s
```

(If that `wait` fails immediately with "no matching resources found", the controller pod
just hasn't been scheduled yet — wait a couple seconds and re-run it.)

Build and load every service image (kind can't pull images that only exist in your local
Docker daemon, so each one has to be explicitly loaded onto the cluster's node):

```bash
for svc in api-gateway order-service inventory-service notification-service analytics-service; do
  docker build -t "$svc:local" "./$svc"
done

kind load docker-image \
  api-gateway:local order-service:local inventory-service:local \
  notification-service:local analytics-service:local \
  --name go-event-platform
```

Generate the dev mTLS certs (if `./certs/` isn't already populated — see
[gRPC mTLS](#grpc-mtls)) and load them as Secrets. Unlike the rest of the manifests, TLS
material is never committed, so this is a script rather than a checked-in Secret YAML:

```bash
./k8s/generate-secrets.sh
```

Apply everything else and wait for it to come up:

```bash
kubectl apply -f k8s/
kubectl wait --for=condition=Ready pod --all -n go-event-platform --timeout=180s
```

api-gateway and Grafana are reachable straight from the host through the Ingress, no
port-forward needed — the same role the `8080:8080` and `3000:3000` port mappings play
in compose:

```bash
curl http://localhost:8090/items/SKU-001
curl -X POST -d '{"sku":"SKU-001","quantity":5}' http://localhost:8090/orders
```

Open http://localhost:8090/grafana/ for the "go-event-platform overview" dashboard (no
login needed). Grafana is configured with `GF_SERVER_ROOT_URL`/`GF_SERVER_SERVE_FROM_SUB_PATH`
to serve correctly under that `/grafana` prefix instead of its own root — if you change
`kind-config.yaml`'s host port, update `GF_SERVER_ROOT_URL` (or `ingress.grafanaRootURL`
in the Helm chart's `values.yaml`) to match.

Everything else stays internal-only, same as compose not publishing notification-service
anywhere but its own port — reach Jaeger and Prometheus with `kubectl port-forward`:

```bash
kubectl port-forward -n go-event-platform svc/jaeger 16686:16686 &      # http://localhost:16686
kubectl port-forward -n go-event-platform svc/prometheus 9090:9090 &    # http://localhost:9090
```

Tear down:

```bash
kind delete cluster --name go-event-platform
```

## Packaging as Helm

`helm/go-event-platform/` packages the same resources as `k8s/` into a chart — same
Deployments/Services/PVCs/Ingress, same `initContainers`, same reliance on
`generate-secrets.sh` for TLS material (Helm doesn't manage that Secret; it's still
created out-of-band, same as the raw-manifest path). The one thing templatized is what
actually needs to vary between installs: each app service's image repository/tag/pull
policy (`values.yaml`) and whether/how the Ingress is exposed. Everything else — env
vars, ports, probes, the Prometheus/Grafana config — is identical to `k8s/` on purpose;
this chart is packaging, not a redesign.

Given a cluster already set up per [Running on Kubernetes](#running-on-kubernetes)
(namespace, ingress-nginx, images loaded, TLS secrets generated), install with:

```bash
helm install go-event-platform helm/go-event-platform --namespace go-event-platform --create-namespace
```

Upgrade after rebuilding an image (`kind load docker-image` first, same as the raw
manifest path). Since the image tag (`:local`) doesn't change, `helm upgrade` sees no
diff in the Deployment spec and won't restart anything on its own -- follow it with a
`rollout restart` for whichever services you rebuilt:

```bash
helm upgrade go-event-platform helm/go-event-platform --namespace go-event-platform
kubectl rollout restart deployment/api-gateway deployment/order-service \
  deployment/inventory-service deployment/notification-service deployment/analytics-service \
  -n go-event-platform
```

(The same applies to the raw-manifest path — `kubectl apply -f k8s/` alone won't pick up
a rebuilt image under an unchanged tag either.)

Uninstall:

```bash
helm uninstall go-event-platform --namespace go-event-platform
```

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
doesn't run as part of a normal `go test`; includes a test that stops and restarts the
`nats` container mid-flow to verify the transactional outbox's durability guarantee):

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

This was intentionally built as the simplest slice that demonstrates the architecture
end to end, then evolved incrementally. That plan is now complete: REST+gRPC sync
communication, NATS async messaging with a transactional outbox, Postgres+Redis storage,
mTLS between order-service and inventory-service, full observability (structured
logging, Prometheus metrics, OTel tracing, Grafana dashboards), and deployment via both
Docker Compose and Kubernetes (raw manifests, Ingress, and a Helm chart).
