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

-- name: GetUserPasswordByID :one
SELECT id, password_hash
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
ON CONFLICT (fridge_id, user_id) DO NOTHING
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

-- name: UpdateUserProfile :one
UPDATE users
SET 
    name = $2,
    dietary_preferences = $3,
    cuisine_preference = $4,
    preferred_language = $5,
    preferred_model = $6,
    updated_at = NOW()
WHERE id = $1
RETURNING id, email, name, dietary_preferences, cuisine_preference, preferred_language, preferred_model, created_at, updated_at;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = NOW()
WHERE id = $1;

-- name: GetUserFullByID :one
SELECT id, email, name, dietary_preferences, cuisine_preference, preferred_language, preferred_model, created_at, updated_at
FROM users
WHERE id = $1;

-- name: RemoveFridgeMember :exec
DELETE FROM fridge_members
WHERE fridge_id = $1 AND user_id = $2;

-- name: CreateOrUpdatePendingRegistration :one
INSERT INTO pending_registrations (email, name, password_hash, verification_code, expires_at, attempts_left)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (email) DO UPDATE SET
    name = EXCLUDED.name,
    password_hash = EXCLUDED.password_hash,
    verification_code = EXCLUDED.verification_code,
    expires_at = EXCLUDED.expires_at,
    attempts_left = EXCLUDED.attempts_left,
    created_at = NOW()
RETURNING id, email, name, password_hash, verification_code, expires_at, attempts_left, created_at;

-- name: GetPendingRegistrationByEmail :one
SELECT id, email, name, password_hash, verification_code, expires_at, attempts_left, created_at
FROM pending_registrations
WHERE email = $1;

-- name: DecrementPendingRegistrationAttempts :one
UPDATE pending_registrations
SET attempts_left = attempts_left - 1
WHERE email = $1
RETURNING attempts_left;

-- name: DeletePendingRegistration :exec
DELETE FROM pending_registrations
WHERE email = $1;

-- name: UpdatePendingRegistrationCode :exec
UPDATE pending_registrations
SET verification_code = $2, expires_at = $3, attempts_left = $4
WHERE email = $1;

-- name: CleanExpiredPendingRegistrations :exec
DELETE FROM pending_registrations
WHERE expires_at < NOW();

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = $1;



