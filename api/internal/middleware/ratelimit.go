// Package middleware provides reusable Gin middleware for the API server.
package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	cleanupInterval = 5 * time.Minute
	idleTimeout     = 10 * time.Minute
)

// entry pairs a rate limiter with the last time it was used.
type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter holds a per-IP token-bucket limiter and the parameters used
// to create new limiters on first request.
type IPRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*entry
	r        rate.Limit // tokens added per second
	b        int        // bucket capacity (burst)
}

// NewIPRateLimiter creates an IPRateLimiter where each IP is allowed r events
// per second with a burst of b. A background goroutine periodically evicts
// entries that have been idle longer than idleTimeout to prevent unbounded
// memory growth.
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	l := &IPRateLimiter{
		limiters: make(map[string]*entry),
		r:        r,
		b:        b,
	}
	go l.cleanupLoop()
	return l
}

// cleanupLoop runs every cleanupInterval and evicts limiters that have not
// been accessed within idleTimeout.
func (l *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip, e := range l.limiters {
			if time.Since(e.lastSeen) > idleTimeout {
				delete(l.limiters, ip)
			}
		}
		l.mu.Unlock()
	}
}

// get returns the limiter for ip, creating one if it doesn't exist yet, and
// updates the entry's lastSeen time for cleanup tracking.
func (l *IPRateLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.limiters[ip]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.r, l.b)}
		l.limiters[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

// Limit returns a Gin middleware that enforces the per-IP rate limit.
// Requests that exceed the limit receive 429 Too Many Requests.
func (l *IPRateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !l.get(ip).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please wait before trying again.",
			})
			return
		}
		c.Next()
	}
}
