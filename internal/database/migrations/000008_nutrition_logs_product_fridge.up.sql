ALTER TABLE nutrition_logs
    ADD COLUMN IF NOT EXISTS product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS fridge_id UUID REFERENCES fridges(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_nutrition_logs_product_id ON nutrition_logs(product_id);
CREATE INDEX IF NOT EXISTS idx_nutrition_logs_fridge_id ON nutrition_logs(fridge_id);
