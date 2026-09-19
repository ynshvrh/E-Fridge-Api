-- name: CreateNutritionLog :one
INSERT INTO nutrition_logs (
    user_id, date, meal_type, food_name, quantity, unit, calories, protein, fat, carbs
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
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
