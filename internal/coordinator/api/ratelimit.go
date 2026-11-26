package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	at     time.Time
}

type Limiter struct {
	mu          sync.Mutex
	perIP       map[string]*bucket
	global      *bucket
	rate        float64
	burst       float64
	globalRate  float64
	globalBurst float64
	ttl         time.Duration
}

func NewLimiter(perMinute, burst, globalPerMinute int) *Limiter {
	if perMinute <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = perMinute
	}
	global := globalPerMinute
	if global <= 0 {
		global = perMinute * 10
	}
	now := time.Now()
	return &Limiter{
		perIP:       make(map[string]*bucket),
		global:      &bucket{tokens: float64(global), at: now},
		rate:        float64(perMinute) / 60,
		burst:       float64(burst),
		globalRate:  float64(global) / 60,
		globalBurst: float64(global),
		ttl:         10 * time.Minute,
	}
}

func (l *Limiter) Allow(now time.Time, key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)

	perIP, ok := l.perIP[key]
	if !ok {
		perIP = &bucket{tokens: l.burst, at: now}
		l.perIP[key] = perIP
	}
	refill(perIP, l.rate, l.burst, now)
	if perIP.tokens < 1 {
		return false, waitFor(perIP, l.rate)
	}

	refill(l.global, l.globalRate, l.globalBurst, now)
	if l.global.tokens < 1 {
		return false, waitFor(l.global, l.globalRate)
	}

	perIP.tokens--
	l.global.tokens--
	return true, 0
}

func (l *Limiter) pruneLocked(now time.Time) {
	for key, value := range l.perIP {
		if now.Sub(value.at) > l.ttl && value.tokens >= l.burst {
			delete(l.perIP, key)
		}
	}
}

func refill(target *bucket, rate, burst float64, now time.Time) {
	if now.Before(target.at) {
		target.at = now
	}
	elapsed := now.Sub(target.at).Seconds()
	target.tokens = math.Min(burst, target.tokens+elapsed*rate)
	target.at = now
}

func waitFor(target *bucket, rate float64) time.Duration {
	if rate <= 0 {
		return time.Minute
	}
	missing := 1 - target.tokens
	return time.Duration(missing/rate*float64(time.Second)) + time.Second
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed, wait := s.Limiter.Allow(s.now(), clientKey(r))
		if !allowed {
			seconds := int(wait.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error":  "слишком много попыток подряд — подождите и повторите",
				"reason": "rate_limited",
			})
			return
		}
		next(w, r)
	}
}

func remoteOf(r *http.Request) string {
	return clientKey(r)
}
