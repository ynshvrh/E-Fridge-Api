package response_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	response.JSON(rec, http.StatusOK, map[string]string{"foo": "bar"})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var res response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected success true, got false")
	}
}

func TestSanitizeMessage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "pq: relation \"users\" does not exist",
			expected: "Внутрішня помилка сервера. Спробуйте пізніше.",
		},
		{
			input:    "sql: syntax error at or near 'SELECT'",
			expected: "Внутрішня помилка сервера. Спробуйте пізніше.",
		},
		{
			input:    "dial tcp 127.0.0.1:5432: connection refused",
			expected: "Внутрішня помилка сервера. Спробуйте пізніше.",
		},
		{
			input:    "Invalid email format",
			expected: "Invalid email format",
		},
	}

	for _, tc := range tests {
		got := response.SanitizeMessage(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeMessage(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestError_SanitizesInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, http.StatusInternalServerError, "pq: column \"foo\" does not exist", "DB_ERROR")

	var res response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if res.Success {
		t.Errorf("expected Success=false")
	}
	if res.Error.Message != "Внутрішня помилка сервера. Спробуйте пізніше." {
		t.Errorf("expected sanitized message, got %q", res.Error.Message)
	}
}

func TestInternalError_StructuredLoggingAndSanitization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	ctx := context.WithValue(req.Context(), chimiddleware.RequestIDKey, "test-req-id-1234")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	err := errors.New("pq: table not found")
	response.InternalError(rec, req, err, "Не вдалося отримати дані")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var res response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if res.Success {
		t.Errorf("expected Success=false")
	}
	if res.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("expected code INTERNAL_ERROR, got %s", res.Error.Code)
	}
	if res.Error.Message != "Не вдалося отримати дані" {
		t.Errorf("expected custom user friendly message, got %s", res.Error.Message)
	}
}
