package auth

import (
	"sync"
	"time"
)

type attemptInfo struct {
	count        int
	firstAttempt time.Time
	lockedUntil  time.Time
}

type LoginRateLimiter struct {
	mu          sync.Mutex
	attempts    map[string]attemptInfo
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
	stop        chan struct{}
	done        chan struct{}
}

func NewLoginRateLimiter() *LoginRateLimiter {
	return &LoginRateLimiter{
		attempts:    make(map[string]attemptInfo),
		maxAttempts: 5,
		window:      5 * time.Minute,
		lockout:     15 * time.Minute,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
}

func (l *LoginRateLimiter) Start() {
	go l.cleanupLoop()
}

func (l *LoginRateLimiter) cleanupLoop() {
	defer close(l.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for l.cleanupTick(ticker.C) {
	}
}

func (l *LoginRateLimiter) cleanupTick(tick <-chan time.Time) bool {
	select {
	case <-l.stop:
		return false
	case <-tick:
		l.cleanup(time.Now())
		return true
	}
}

func (l *LoginRateLimiter) Stop() {
	select {
	case <-l.done:
		return
	default:
	}
	close(l.stop)
	<-l.done
}

func (l *LoginRateLimiter) IsAllowed(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	info, ok := l.attempts[ip]
	if !ok {
		return true, 0
	}
	if !info.lockedUntil.IsZero() {
		if now.Before(info.lockedUntil) {
			return false, time.Until(info.lockedUntil)
		}
		delete(l.attempts, ip)
		return true, 0
	}
	if now.Sub(info.firstAttempt) > l.window {
		delete(l.attempts, ip)
		return true, 0
	}
	return info.count < l.maxAttempts, 0
}

func (l *LoginRateLimiter) RecordFailure(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	info := l.attempts[ip]
	if info.firstAttempt.IsZero() || now.Sub(info.firstAttempt) > l.window {
		info = attemptInfo{firstAttempt: now}
	}
	info.count++
	if info.count >= l.maxAttempts {
		info.lockedUntil = now.Add(l.lockout)
	}
	l.attempts[ip] = info
	return info.count
}

func (l *LoginRateLimiter) RecordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

func (l *LoginRateLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, info := range l.attempts {
		if (!info.lockedUntil.IsZero() && now.After(info.lockedUntil)) || now.Sub(info.firstAttempt) > l.window {
			delete(l.attempts, ip)
		}
	}
}
