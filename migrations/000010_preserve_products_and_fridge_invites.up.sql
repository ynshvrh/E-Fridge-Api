-- 1. Preserve products when member user is deleted
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_created_by_fkey;
ALTER TABLE products ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE products ADD CONSTRAINT products_created_by_fkey FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

-- 2. Create fridge_invites table for link invitations
CREATE TABLE IF NOT EXISTS fridge_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fridge_id UUID NOT NULL REFERENCES fridges(id) ON DELETE CASCADE,
    token VARCHAR(64) NOT NULL UNIQUE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fridge_invites_token ON fridge_invites(token);
CREATE INDEX IF NOT EXISTS idx_fridge_invites_fridge_id ON fridge_invites(fridge_id);
