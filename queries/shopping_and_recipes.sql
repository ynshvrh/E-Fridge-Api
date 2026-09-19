-- name: CreateShoppingItem :one
INSERT INTO shopping_items (
    fridge_id, name, category, quantity, unit, is_bought, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetShoppingItemByID :one
SELECT * FROM shopping_items
WHERE id = $1 AND fridge_id = $2;

-- name: ListShoppingItemsByFridge :many
SELECT * FROM shopping_items
WHERE fridge_id = $1
ORDER BY is_bought ASC, created_at DESC;

-- name: UpdateShoppingItem :one
UPDATE shopping_items
SET name = $3,
    category = $4,
    quantity = $5,
    unit = $6,
    is_bought = $7,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING *;

-- name: ToggleShoppingItemBought :one
UPDATE shopping_items
SET is_bought = $3,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING *;

-- name: DeleteShoppingItem :exec
DELETE FROM shopping_items
WHERE id = $1 AND fridge_id = $2;

-- name: DeleteBoughtShoppingItems :exec
DELETE FROM shopping_items
WHERE fridge_id = $1 AND is_bought = TRUE;

-- name: ClearShoppingList :exec
DELETE FROM shopping_items
WHERE fridge_id = $1;

-- name: CreateSavedRecipe :one
INSERT INTO saved_recipes (
    user_id, fridge_id, title, description, ingredients, steps,
    calories, protein, fat, carbs, prep_time_mins, cook_time_mins, servings
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: GetSavedRecipeByID :one
SELECT * FROM saved_recipes
WHERE id = $1 AND user_id = $2;

-- name: ListSavedRecipesByUser :many
SELECT * FROM saved_recipes
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: DeleteSavedRecipe :exec
DELETE FROM saved_recipes
WHERE id = $1 AND user_id = $2;
