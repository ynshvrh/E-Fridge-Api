CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fridge_id UUID NOT NULL REFERENCES fridges(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    category VARCHAR(50) NOT NULL DEFAULT 'other',
    quantity DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    unit VARCHAR(20) NOT NULL DEFAULT 'pcs',
    expiry_date DATE,
    calories INTEGER NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    notes TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_products_fridge_id ON products(fridge_id);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_expiry_date ON products(expiry_date);
