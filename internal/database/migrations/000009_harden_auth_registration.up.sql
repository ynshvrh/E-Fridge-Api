ALTER TABLE pending_registrations ADD COLUMN IF NOT EXISTS attempts_left INT NOT NULL DEFAULT 5;
