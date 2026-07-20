CREATE TABLE IF NOT EXISTS items (
    sku      TEXT PRIMARY KEY,
    name     TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity >= 0)
);

INSERT INTO items (sku, name, quantity) VALUES
    ('SKU-001', 'Widget', 100),
    ('SKU-002', 'Gadget', 50)
ON CONFLICT (sku) DO NOTHING;
