// Copied from github.com/jiayaoqijia/ottie/pkg/gateway/ratelimit.go
// Copyright (c) 2026 Ottie contributors — MIT License

package gateway

import (
	"net"
	"sync"
	"time"
)

// RateLimitConfig holds the configuration for the rate limiter.
type RateLimitConfig struct {
	MaxAttempts    int
	WindowSeconds  int
	LockoutSeconds int
}

type rlEntry struct {
	timestamps  []time.Time
	lockedUntil time.Time
}

// RateLimiter tracks request rates per scope+IP and enforces limits.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rlEntry
	config  RateLimitConfig
	stopCh  chan struct{}
}

// NewRateLimiter creates a RateLimiter and starts a background prune loop.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rlEntry),
		config:  cfg,
		stopCh:  make(chan struct{}),
	}
	go rl.pruneLoop()
	return rl
}

// Allow returns true if the request should be allowed.
// Loopback addresses are always allowed.
func (rl *RateLimiter) Allow(scope, ip string) bool {
	if isLoopback(ip) {
		return true
	}

	key := scope + ":" + ip
	now := time.Now()
	window := time.Duration(rl.config.WindowSeconds) * time.Second

	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.entries[key]
	if !ok {
		e = &rlEntry{}
		rl.entries[key] = e
	}

	if now.Before(e.lockedUntil) {
		return false
	}

	cutoff := now.Add(-window)
	filtered := e.timestamps[:0]
	for _, ts := range e.timestamps {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	e.timestamps = filtered

	if len(e.timestamps) >= rl.config.MaxAttempts {
		e.lockedUntil = now.Add(
			time.Duration(rl.config.LockoutSeconds) * time.Second,
		)
		e.timestamps = nil
		return false
	}

	e.timestamps = append(e.timestamps, now)
	return true
}

// Stop stops the background prune goroutine.
func (rl *RateLimiter) Stop() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	select {
	case <-rl.stopCh:
	default:
		close(rl.stopCh)
	}
}

func (rl *RateLimiter) pruneLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.prune()
		}
	}
}

func (rl *RateLimiter) prune() {
	now := time.Now()
	window := time.Duration(rl.config.WindowSeconds) * time.Second

	rl.mu.Lock()
	defer rl.mu.Unlock()

	for key, e := range rl.entries {
		if now.After(e.lockedUntil) {
			cutoff := now.Add(-window)
			hasRecent := false
			for _, ts := range e.timestamps {
				if ts.After(cutoff) {
					hasRecent = true
					break
				}
			}
			if !hasRecent {
				delete(rl.entries, key)
			}
		}
	}
}

func isLoopback(ip string) bool {
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}
	if ip == "localhost" {
		return true
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}
