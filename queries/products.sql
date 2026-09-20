-- name: CreateProduct :one
INSERT INTO products (
    fridge_id, name, category, quantity, unit, expiry_date, calories, protein, fat, carbs, notes, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetProductByID :one
SELECT * FROM products
WHERE id = $1 AND fridge_id = $2;

-- name: ListProductsByFridge :many
SELECT * FROM products
WHERE fridge_id = $1
ORDER BY expiry_date ASC NULLS LAST, created_at DESC;

-- name: UpdateProduct :one
UPDATE products
SET name = $3,
    category = $4,
    quantity = $5,
    unit = $6,
    expiry_date = $7,
    calories = $8,
    protein = $9,
    fat = $10,
    carbs = $11,
    notes = $12,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING *;

-- name: UpdateProductQuantity :one
UPDATE products
SET quantity = $3,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING *;

-- name: UpdateProductQuantityAndUnit :one
UPDATE products
SET quantity = $3,
    unit = $4,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING *;


-- name: DeleteProduct :exec
DELETE FROM products
WHERE id = $1 AND fridge_id = $2;

-- name: DeleteAllProductsByFridge :exec
DELETE FROM products
WHERE fridge_id = $1;
