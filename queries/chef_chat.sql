-- name: ListChefMessages :many
SELECT id, fridge_id, user_id, role, content, recipe_data, shopping_suggestions, created_at
FROM chef_messages
WHERE fridge_id = $1
ORDER BY created_at ASC
LIMIT $2;

-- name: CreateChefMessage :one
INSERT INTO chef_messages (
    fridge_id, user_id, role, content, recipe_data, shopping_suggestions
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, fridge_id, user_id, role, content, recipe_data, shopping_suggestions, created_at;

-- name: ClearChefMessages :exec
DELETE FROM chef_messages
WHERE fridge_id = $1;
