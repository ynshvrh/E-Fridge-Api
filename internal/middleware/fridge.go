package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/ynshvrh/E-Fridge-Api/internal/db"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

const (
	fridgeIDKey   contextKey = "fridge_id"
	fridgeRoleKey contextKey = "fridge_role"
)

func RequireFridge(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserID(r.Context())
			if !ok {
				response.Error(w, http.StatusUnauthorized, "User context missing", "UNAUTHORIZED")
				return
			}

			fridgeIDStr := r.Header.Get("X-Fridge-Id")
			if fridgeIDStr == "" {
				fridgeIDStr = r.URL.Query().Get("fridge_id")
			}

			if fridgeIDStr == "" {
				response.Error(w, http.StatusBadRequest, "Missing X-Fridge-Id header or fridge_id parameter", "FRIDGE_ID_REQUIRED")
				return
			}

			fridgeID, err := uuid.Parse(fridgeIDStr)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "Invalid fridge ID format", "INVALID_FRIDGE_ID")
				return
			}

			member, err := queries.GetFridgeMember(r.Context(), db.GetFridgeMemberParams{
				FridgeID: fridgeID,
				UserID:   userID,
			})
			if err != nil {
				response.Error(w, http.StatusForbidden, "You do not have access to this fridge", "FORBIDDEN")
				return
			}

			ctx := context.WithValue(r.Context(), fridgeIDKey, member.FridgeID)
			ctx = context.WithValue(ctx, fridgeRoleKey, member.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetFridgeID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(fridgeIDKey).(uuid.UUID)
	return id, ok
}

func GetFridgeRole(ctx context.Context) string {
	if role, ok := ctx.Value(fridgeRoleKey).(string); ok {
		return role
	}
	return ""
}
