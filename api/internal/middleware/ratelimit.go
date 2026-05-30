// Package middleware provides reusable Gin middleware for the API server.
package middleware

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// IPRateLimiter holds a per-IP token-bucket limiter and the parameters used
// to create new limiters on first request.
type IPRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit // tokens added per second
	b        int        // bucket capacity (burst)
}

// NewIPRateLimiter creates an IPRateLimiter where each IP is allowed r events
// per second with a burst of b.
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
	}
}

// get returns the limiter for ip, creating one if it doesn't exist yet.
func (l *IPRateLimiter) get(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if lim, ok := l.limiters[ip]; ok {
		return lim
	}
	lim := rate.NewLimiter(l.r, l.b)
	l.limiters[ip] = lim
	return lim
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
