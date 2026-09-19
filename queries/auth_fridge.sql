-- name: CreateUser :one
INSERT INTO users (email, name, password_hash)
VALUES ($1, $2, $3)
RETURNING id, email, name, password_hash, created_at, updated_at;

-- name: GetUserByEmail :one
SELECT id, email, name, password_hash, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, name, created_at, updated_at
FROM users
WHERE id = $1;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at;

-- name: GetValidRefreshTokenByHash :one
SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
FROM refresh_tokens
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE token_hash = $1;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateFridge :one
INSERT INTO fridges (name, owner_id)
VALUES ($1, $2)
RETURNING id, name, owner_id, created_at, updated_at;

-- name: AddFridgeMember :one
INSERT INTO fridge_members (fridge_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (fridge_id, user_id) DO UPDATE SET role = EXCLUDED.role
RETURNING fridge_id, user_id, role, joined_at;

-- name: GetFridgesByUserID :many
SELECT f.id, f.name, f.owner_id, f.created_at, f.updated_at, fm.role
FROM fridges f
JOIN fridge_members fm ON f.id = fm.fridge_id
WHERE fm.user_id = $1
ORDER BY f.created_at ASC;

-- name: GetFridgeByID :one
SELECT id, name, owner_id, created_at, updated_at
FROM fridges
WHERE id = $1;

-- name: GetFridgeMember :one
SELECT fridge_id, user_id, role, joined_at
FROM fridge_members
WHERE fridge_id = $1 AND user_id = $2;

-- name: GetFridgeMembers :many
SELECT u.id, u.name, u.email, fm.role, fm.joined_at
FROM fridge_members fm
JOIN users u ON fm.user_id = u.id
WHERE fm.fridge_id = $1
ORDER BY fm.joined_at ASC;

-- name: DeleteFridge :exec
DELETE FROM fridges
WHERE id = $1;
