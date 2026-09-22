package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMaxBodySize(t *testing.T) {
	mw := MaxBodySize(10)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Body <= 10 bytes should pass
	req := httptest.NewRequest("POST", "/test", strings.NewReader("short"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Case 2: Body > 10 bytes should fail
	req2 := httptest.NewRequest("POST", "/test", strings.NewReader("this string is definitely too long"))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when reading body exceeding limit, got %d", rec2.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(2, 500*time.Millisecond, "Too many requests", "RATE_LIMIT")
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// Request 1: OK
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("req 1: expected 200, got %d", rec1.Code)
	}

	// Request 2: OK
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("req 2: expected 200, got %d", rec2.Code)
	}

	// Request 3: Exceeded limit -> 429
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3: expected 429, got %d", rec3.Code)
	}
	if rec3.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header to be set")
	}

	// Wait for window to expire
	time.Sleep(550 * time.Millisecond)

	// Request 4: OK again
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req)
	if rec4.Code != http.StatusOK {
		t.Fatalf("req 4 after window: expected 200, got %d", rec4.Code)
	}
}

func TestAIGuard(t *testing.T) {
	guard := NewAIGuard(200*time.Millisecond, 5, 1*time.Minute)
	userID := uuid.New()

	handler := guard.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func() *http.Request {
		req := httptest.NewRequest("POST", "/api/v1/chef/generate", nil)
		ctx := context.WithValue(req.Context(), userIDKey, userID)
		return req.WithContext(ctx)
	}

	// Test 1: In-flight concurrency prevention
	var wg sync.WaitGroup
	var codes []int
	var codesMu sync.Mutex

	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, makeReq())
			codesMu.Lock()
			codes = append(codes, rec.Code)
			codesMu.Unlock()
		}()
	}
	wg.Wait()

	has200 := false
	has429 := false
	for _, code := range codes {
		if code == http.StatusOK {
			has200 = true
		}
		if code == http.StatusTooManyRequests {
			has429 = true
		}
	}

	if !has200 || !has429 {
		t.Fatalf("expected one 200 and one 429 during concurrent generation, got codes: %v", codes)
	}

	// Test 2: Cooldown check immediately after generation finished
	recCooldown := httptest.NewRecorder()
	handler.ServeHTTP(recCooldown, makeReq())
	if recCooldown.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 due to cooldown, got %d", recCooldown.Code)
	}

	// Test 3: After cooldown expires, request succeeds
	time.Sleep(250 * time.Millisecond)
	recAfterCooldown := httptest.NewRecorder()
	handler.ServeHTTP(recAfterCooldown, makeReq())
	if recAfterCooldown.Code != http.StatusOK {
		t.Fatalf("expected 200 after cooldown, got %d", recAfterCooldown.Code)
	}
}

func TestGetClientIP_SpoofingProtection(t *testing.T) {
	// Case 1: Direct public connection with spoofed headers -> ignore headers
	req1 := httptest.NewRequest("POST", "/test", nil)
	req1.RemoteAddr = "203.0.113.195:54321"
	req1.Header.Set("X-Forwarded-For", "10.0.0.1")
	req1.Header.Set("X-Real-IP", "10.0.0.2")

	ip1 := GetClientIP(req1)
	if ip1 != "203.0.113.195" {
		t.Fatalf("expected remote addr 203.0.113.195 to be used against spoofing, got %s", ip1)
	}

	// Case 2: Trusted local reverse proxy with X-Real-IP -> trust X-Real-IP
	req2 := httptest.NewRequest("POST", "/test", nil)
	req2.RemoteAddr = "172.18.0.2:54321" // Docker bridge private network
	req2.Header.Set("X-Real-IP", "198.51.100.5")

	ip2 := GetClientIP(req2)
	if ip2 != "198.51.100.5" {
		t.Fatalf("expected X-Real-IP 198.51.100.5 from private proxy, got %s", ip2)
	}

	// Case 3: Trusted loopback with X-Forwarded-For -> trust X-Forwarded-For
	req3 := httptest.NewRequest("POST", "/test", nil)
	req3.RemoteAddr = "127.0.0.1:54321"
	req3.Header.Set("X-Forwarded-For", "198.51.100.6, 172.18.0.2")

	ip3 := GetClientIP(req3)
	if ip3 != "198.51.100.6" {
		t.Fatalf("expected client IP from X-Forwarded-For, got %s", ip3)
	}
}

func TestEmailAndIPKey(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", strings.NewReader(`{"email":"User@Example.Com","code":"123456"}`))
	req.RemoteAddr = "203.0.113.1:1234"

	key := EmailAndIPKey(req)
	expected := "user@example.com:203.0.113.1"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}

	// Ensure body is still readable by subsequent handlers
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("failed to read restored body: %v", err)
	}
	if !strings.Contains(string(bodyBytes), "123456") {
		t.Fatalf("restored body content mismatch: %s", string(bodyBytes))
	}
}
