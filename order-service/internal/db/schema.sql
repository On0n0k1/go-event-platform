CREATE TABLE IF NOT EXISTS orders (
    id         TEXT PRIMARY KEY,
    sku        TEXT NOT NULL,
    quantity   INTEGER NOT NULL CHECK (quantity > 0),
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Transactional outbox: written in the same DB transaction as the order that
-- caused it, so "order exists" and "event is durably queued to publish" can
-- never diverge. A relay polls unpublished rows and publishes them to NATS.
CREATE TABLE IF NOT EXISTS outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    subject      TEXT NOT NULL,
    payload      JSONB NOT NULL,
    trace_parent TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished
    ON outbox_events (id)
    WHERE published_at IS NULL;
