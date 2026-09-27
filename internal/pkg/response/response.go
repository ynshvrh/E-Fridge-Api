package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    data,
	})
}

// SanitizeMessage ensures internal SQL or system errors do not leak to the client.
func SanitizeMessage(message string) string {
	lower := strings.ToLower(message)
	leakIndicators := []string{
		"pq:", "sql:", "pgx:", "syntax error", "connection refused",
		"dial tcp", "column \"", "relation \"", "table \"",
		"select ", "insert into ", "update ", "delete from ",
		"duplicate key value violates unique constraint",
	}

	for _, indicator := range leakIndicators {
		if strings.Contains(lower, indicator) {
			return "Внутрішня помилка сервера. Спробуйте пізніше."
		}
	}
	return message
}

func Error(w http.ResponseWriter, statusCode int, message string, code ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errCode := ""
	if len(code) > 0 {
		errCode = code[0]
	}

	// Always sanitize 5xx messages
	if statusCode >= 500 {
		message = SanitizeMessage(message)
	}

	_ = json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Error: &APIError{
			Code:    errCode,
			Message: message,
		},
	})
}

// InternalError logs the actual error with request_id and sends a sanitized 500 response.
func InternalError(w http.ResponseWriter, r *http.Request, err error, userFriendlyMsg ...string) {
	var reqID string
	var path, method string
	if r != nil {
		reqID = chimiddleware.GetReqID(r.Context())
		if r.URL != nil {
			path = r.URL.Path
		}
		method = r.Method
	}

	slog.Error("Internal server error",
		"error", err,
		"request_id", reqID,
		"path", path,
		"method", method,
	)

	msg := "Внутрішня помилка сервера. Спробуйте пізніше."
	if len(userFriendlyMsg) > 0 && userFriendlyMsg[0] != "" {
		msg = SanitizeMessage(userFriendlyMsg[0])
	}

	Error(w, http.StatusInternalServerError, msg, "INTERNAL_ERROR")
}

