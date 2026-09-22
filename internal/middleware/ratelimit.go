package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ynshvrh/E-Fridge-Api/internal/pkg/response"
)

// MaxBodySize restricts incoming request bodies to maxBytes.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// isPrivateOrLoopback returns true if ipStr is a loopback or private network address.
func isPrivateOrLoopback(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// GetClientIP extracts the client IP address from the request.
// Reverse proxy headers (X-Real-IP, X-Forwarded-For) are only trusted if the direct connection (RemoteAddr)
// originates from a loopback or private network address (e.g. local Nginx or Docker bridge network).
func GetClientIP(r *http.Request) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	// Only trust forwarded headers if the peer is a trusted local/private proxy
	if isPrivateOrLoopback(remoteHost) {
		// Prefer X-Real-IP set by trusted reverse proxy (e.g. Nginx proxy_set_header X-Real-IP $remote_addr)
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if ip := net.ParseIP(xri); ip != nil {
				return xri
			}
		}

		// Fallback to X-Forwarded-For if behind a proxy chain
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for _, p := range parts {
				ipStr := strings.TrimSpace(p)
				if ipStr != "" && net.ParseIP(ipStr) != nil {
					return ipStr
				}
			}
		}
	}

	return remoteHost
}

// EmailAndIPKey extracts the email from the JSON body (if present) and combines it with the client IP
// in the format "<email>:<ip>". If no email is present or the body cannot be parsed, it falls back to IP.
// The request body is preserved and can be read by downstream handlers.
func EmailAndIPKey(r *http.Request) string {
	ip := GetClientIP(r)
	if r.Body == nil {
		return ip
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil || len(bodyBytes) == 0 {
		return ip
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err == nil && strings.TrimSpace(payload.Email) != "" {
		email := strings.ToLower(strings.TrimSpace(payload.Email))
		return email + ":" + ip
	}

	return ip
}

// RateLimiter tracks requests per client key using a sliding window.
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
	message  string
	code     string
	keyFn    func(r *http.Request) string
}

// NewRateLimiter creates a new sliding window rate limiter.
func NewRateLimiter(limit int, window time.Duration, message, code string, keyFn ...func(r *http.Request) string) *RateLimiter {
	fn := GetClientIP
	if len(keyFn) > 0 && keyFn[0] != nil {
		fn = keyFn[0]
	}

	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
		message:  message,
		code:     code,
		keyFn:    fn,
	}

	// Periodic cleanup of stale entries every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window * 2)
	for key, timestamps := range rl.requests {
		if len(timestamps) == 0 || timestamps[len(timestamps)-1].Before(cutoff) {
			delete(rl.requests, key)
		}
	}
}

// Middleware returns the HTTP middleware handler.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rl.keyFn(r)
			if key == "" {
				key = "unknown"
			}

			rl.mu.Lock()
			now := time.Now()
			cutoff := now.Add(-rl.window)

			// Filter out expired timestamps
			var valid []time.Time
			for _, t := range rl.requests[key] {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}

			if len(valid) >= rl.limit {
				oldest := valid[0]
				retryAfter := int(time.Until(oldest.Add(rl.window)).Seconds()) + 1
				rl.requests[key] = valid
				rl.mu.Unlock()

				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				response.Error(w, http.StatusTooManyRequests, rl.message, rl.code)
				return
			}

			valid = append(valid, now)
			rl.requests[key] = valid
			rl.mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// AIGuard protects AI generation endpoints:
// 1. Prevents concurrent generation by the same user/client (in-flight lock)
// 2. Enforces a cooldown between generations (e.g. 15 seconds)
// 3. Applies a sliding window limit (e.g. max 10 requests per 5 minutes)
type AIGuard struct {
	mu        sync.Mutex
	inFlight  map[string]bool
	lastEnded map[string]time.Time
	requests  map[string][]time.Time
	cooldown  time.Duration
	maxBurst  int
	window    time.Duration
}

// NewAIGuard creates a new AI rate limiter and concurrency guard.
func NewAIGuard(cooldown time.Duration, maxBurst int, window time.Duration) *AIGuard {
	guard := &AIGuard{
		inFlight:  make(map[string]bool),
		lastEnded: make(map[string]time.Time),
		requests:  make(map[string][]time.Time),
		cooldown:  cooldown,
		maxBurst:  maxBurst,
		window:    window,
	}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			guard.cleanup()
		}
	}()

	return guard
}

func (g *AIGuard) cleanup() {
	g.mu.Lock()
	defer g.mu.Unlock()

	cutoff := time.Now().Add(-g.window * 2)
	for key, timestamps := range g.requests {
		if len(timestamps) == 0 || timestamps[len(timestamps)-1].Before(cutoff) {
			if !g.inFlight[key] {
				delete(g.requests, key)
				delete(g.lastEnded, key)
			}
		}
	}
}

func (g *AIGuard) getKey(r *http.Request) string {
	if userID, ok := GetUserID(r.Context()); ok {
		return userID.String()
	}
	return GetClientIP(r)
}

// Middleware wraps an AI endpoint with concurrency protection, cooldown, and burst limits.
func (g *AIGuard) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := g.getKey(r)

			g.mu.Lock()

			// 1. Check in-flight generation (concurrent duplicate request)
			if g.inFlight[key] {
				g.mu.Unlock()
				response.Error(w, http.StatusTooManyRequests,
					"Генерація вже виконується. Будь ласка, зачекайте завершення попереднього запиту.",
					"AI_GENERATION_IN_PROGRESS")
				return
			}

			// 2. Check cooldown from previous completion
			if lastEnd, exists := g.lastEnded[key]; exists {
				elapsed := time.Since(lastEnd)
				if elapsed < g.cooldown {
					remaining := int((g.cooldown - elapsed).Seconds()) + 1
					g.mu.Unlock()
					w.Header().Set("Retry-After", strconv.Itoa(remaining))
					response.Error(w, http.StatusTooManyRequests,
						fmt.Sprintf("Будь ласка, зачекайте %d с. перед наступною генерацією.", remaining),
						"AI_COOLDOWN")
					return
				}
			}

			// 3. Check burst rate limit in sliding window
			now := time.Now()
			cutoff := now.Add(-g.window)
			var valid []time.Time
			for _, t := range g.requests[key] {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}

			if len(valid) >= g.maxBurst {
				oldest := valid[0]
				retryAfter := int(time.Until(oldest.Add(g.window)).Seconds()) + 1
				g.requests[key] = valid
				g.mu.Unlock()

				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				response.Error(w, http.StatusTooManyRequests,
					"Перевищено ліміт генерацій до ШІ. Спробуйте пізніше.",
					"AI_RATE_LIMIT")
				return
			}

			// Mark as in-flight
			g.inFlight[key] = true
			valid = append(valid, now)
			g.requests[key] = valid
			g.mu.Unlock()

			// Ensure in-flight is cleared and lastEnded is recorded on exit
			defer func() {
				g.mu.Lock()
				g.inFlight[key] = false
				g.lastEnded[key] = time.Now()
				g.mu.Unlock()
			}()

			next.ServeHTTP(w, r)
		})
	}
}
