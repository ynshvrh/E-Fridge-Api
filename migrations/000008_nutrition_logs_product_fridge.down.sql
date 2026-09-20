DROP INDEX IF EXISTS idx_nutrition_logs_fridge_id;
DROP INDEX IF EXISTS idx_nutrition_logs_product_id;
ALTER TABLE nutrition_logs
    DROP COLUMN IF EXISTS fridge_id,
    DROP COLUMN IF EXISTS product_id;
