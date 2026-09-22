DROP TABLE IF EXISTS fridge_invites;

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_created_by_fkey;
ALTER TABLE products ADD CONSTRAINT products_created_by_fkey FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE;
