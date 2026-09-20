package middleware

import (
	"fmt"
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

// GetClientIP extracts the client IP address from the request.
func GetClientIP(r *http.Request) string {
	// First check X-Forwarded-For if behind a proxy
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Then check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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
