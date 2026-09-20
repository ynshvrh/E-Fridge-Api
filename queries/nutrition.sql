-- name: CreateNutritionLog :one
INSERT INTO nutrition_logs (
    user_id, date, meal_type, food_name, quantity, unit, calories, protein, fat, carbs, product_id, fridge_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetNutritionLogByID :one
SELECT * FROM nutrition_logs
WHERE id = $1 AND user_id = $2;

-- name: UpdateNutritionLog :one
UPDATE nutrition_logs
SET meal_type = $3,
    food_name = $4,
    quantity = $5,
    unit = $6,
    calories = $7,
    protein = $8,
    fat = $9,
    carbs = $10
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ListNutritionLogsByDate :many
SELECT * FROM nutrition_logs
WHERE user_id = $1 AND date = $2
ORDER BY logged_at ASC;

-- name: DeleteNutritionLog :exec
DELETE FROM nutrition_logs
WHERE id = $1 AND user_id = $2;

-- name: GetNutritionGoals :one
SELECT * FROM user_nutrition_goals
WHERE user_id = $1;

-- name: UpsertNutritionGoals :one
INSERT INTO user_nutrition_goals (user_id, calorie_target, protein_target, fat_target, carbs_target, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (user_id) DO UPDATE
SET calorie_target = EXCLUDED.calorie_target,
    protein_target = EXCLUDED.protein_target,
    fat_target = EXCLUDED.fat_target,
    carbs_target = EXCLUDED.carbs_target,
    updated_at = NOW()
RETURNING *;
