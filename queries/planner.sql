-- name: ListMealPlansByFridgeAndDateRange :many
SELECT id, fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, is_completed, notes, created_at, updated_at
FROM meal_plans
WHERE fridge_id = $1 AND date >= $2 AND date <= $3
ORDER BY date ASC, 
    CASE meal_type 
        WHEN 'breakfast' THEN 1 
        WHEN 'lunch' THEN 2 
        WHEN 'dinner' THEN 3 
        ELSE 4 
    END;

-- name: GetMealPlanByID :one
SELECT id, fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, is_completed, notes, created_at, updated_at
FROM meal_plans
WHERE id = $1 AND fridge_id = $2;

-- name: CreateMealPlan :one
INSERT INTO meal_plans (
    fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, notes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING id, fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, is_completed, notes, created_at, updated_at;

-- name: UpdateMealPlan :one
UPDATE meal_plans
SET 
    date = $3,
    meal_type = $4,
    recipe_title = $5,
    recipe_id = $6,
    calories = $7,
    protein = $8,
    fat = $9,
    carbs = $10,
    notes = $11,
    updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING id, fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, is_completed, notes, created_at, updated_at;

-- name: ToggleMealPlanCompleted :one
UPDATE meal_plans
SET is_completed = $3, updated_at = NOW()
WHERE id = $1 AND fridge_id = $2
RETURNING id, fridge_id, user_id, date, meal_type, recipe_title, recipe_id, calories, protein, fat, carbs, is_completed, notes, created_at, updated_at;

-- name: DeleteMealPlan :exec
DELETE FROM meal_plans
WHERE id = $1 AND fridge_id = $2;

-- name: ClearMealPlansByDateRange :exec
DELETE FROM meal_plans
WHERE fridge_id = $1 AND date >= $2 AND date <= $3;
