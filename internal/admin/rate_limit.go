package admin

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type requestLimiter struct {
	mu          sync.Mutex
	rate, burst float64
	maximum     int
	clients     map[string]*requestBucket
}

type requestBucket struct {
	tokens  float64
	updated time.Time
}

func newRequestLimiter(rate, burst, maximum int) *requestLimiter {
	return &requestLimiter{rate: float64(rate), burst: float64(burst), maximum: maximum, clients: make(map[string]*requestBucket)}
}

func (l *requestLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, found := l.clients[key]
	if !found {
		if len(l.clients) >= l.maximum {
			oldestKey := ""
			var oldest time.Time
			for candidate, existing := range l.clients {
				if oldestKey == "" || existing.updated.Before(oldest) {
					oldestKey, oldest = candidate, existing.updated
				}
			}
			delete(l.clients, oldestKey)
		}
		l.clients[key] = &requestBucket{tokens: l.burst - 1, updated: now}
		return true
	}
	bucket.tokens += now.Sub(bucket.updated).Seconds() * l.rate
	if bucket.tokens > l.burst {
		bucket.tokens = l.burst
	}
	bucket.updated = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func remoteKey(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}
