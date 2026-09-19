CREATE TABLE IF NOT EXISTS meal_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fridge_id UUID NOT NULL REFERENCES fridges(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    meal_type VARCHAR(50) NOT NULL, -- breakfast, lunch, dinner, snack
    recipe_title VARCHAR(255) NOT NULL,
    recipe_id UUID REFERENCES saved_recipes(id) ON DELETE SET NULL,
    calories INTEGER NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_completed BOOLEAN NOT NULL DEFAULT FALSE,
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_meal_plans_fridge_date ON meal_plans(fridge_id, date);
CREATE INDEX IF NOT EXISTS idx_meal_plans_user ON meal_plans(user_id);

CREATE TABLE IF NOT EXISTS chef_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fridge_id UUID NOT NULL REFERENCES fridges(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL, -- user, assistant
    content TEXT NOT NULL,
    recipe_data JSONB,
    shopping_suggestions JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chef_messages_fridge ON chef_messages(fridge_id, created_at ASC);

ALTER TABLE users ADD COLUMN IF NOT EXISTS dietary_preferences TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS cuisine_preference VARCHAR(100) NOT NULL DEFAULT 'any';
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_language VARCHAR(10) NOT NULL DEFAULT 'uk';
ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_model VARCHAR(100) NOT NULL DEFAULT '';
