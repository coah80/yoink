package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

var (
	rateLimitStore = make(map[string][]time.Time)
	rateLimitMu    sync.Mutex
)

func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := util.GetClientIP(r)
		allowed, remaining, resetIn := checkRateLimit(ip)

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", config.RateLimitMax))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if !allowed {
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetIn))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Too many requests. Please slow down.",
				"resetIn": resetIn,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

const maxRateLimitEntries = 100000

func checkRateLimit(ip string) (allowed bool, remaining int, resetIn int) {
	rateLimitMu.Lock()
	defer rateLimitMu.Unlock()

	now := time.Now()
	windowStart := now.Add(-config.RateLimitWindow)

	requests := rateLimitStore[ip]
	filtered := requests[:0]
	for _, t := range requests {
		if t.After(windowStart) {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) >= config.RateLimitMax {
		resetSec := int(filtered[0].Add(config.RateLimitWindow).Sub(now).Seconds()) + 1
		rateLimitStore[ip] = filtered
		return false, 0, resetSec
	}

	if len(rateLimitStore) >= maxRateLimitEntries {
		rateLimitStore[ip] = filtered
		return false, 0, 60
	}

	filtered = append(filtered, now)
	rateLimitStore[ip] = filtered
	return true, config.RateLimitMax - len(filtered), 0
}

func StartRateLimitCleanup() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			rateLimitMu.Lock()
			now := time.Now()
			windowStart := now.Add(-config.RateLimitWindow)
			for ip, requests := range rateLimitStore {
				filtered := requests[:0]
				for _, t := range requests {
					if t.After(windowStart) {
						filtered = append(filtered, t)
					}
				}
				if len(filtered) == 0 {
					delete(rateLimitStore, ip)
				} else {
					rateLimitStore[ip] = filtered
				}
			}
			rateLimitMu.Unlock()
		}
	}()
}
