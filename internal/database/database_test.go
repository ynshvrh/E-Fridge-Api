package database_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ynshvrh/E-Fridge-Api/internal/database"
	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	return m.err
}

func TestHealthHandler_Success(t *testing.T) {
	pinger := &mockPinger{err: nil}
	handler := database.HealthHandler(pinger, "Test-Service")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var res response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !res.Success {
		t.Errorf("expected success true, got false")
	}

	dataMap, ok := res.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", res.Data)
	}

	if dataMap["database"] != "connected" {
		t.Errorf("expected database=connected, got %v", dataMap["database"])
	}
}

func TestHealthHandler_DatabaseFailure(t *testing.T) {
	pinger := &mockPinger{err: errors.New("connection refused")}
	handler := database.HealthHandler(pinger, "Test-Service")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", rec.Code)
	}

	var res response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Success {
		t.Errorf("expected success false, got true")
	}

	if res.Error == nil || res.Error.Code != "SERVICE_UNAVAILABLE" {
		t.Errorf("expected error code SERVICE_UNAVAILABLE, got %+v", res.Error)
	}
}
