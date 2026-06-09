package server

import (
	"sync"
	"time"
)

const (
	lockThreshold  = 5                // 连续失败达到该次数后锁定
	failureWindow  = 15 * time.Minute // 超过该静默时长后失败计数衰减归零
	limiterMaxKeys = 4096             // 超过该数量时触发一次过期条目清扫
)

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptState
	now      func() time.Time
}

type attemptState struct {
	failures  int
	lockedTil time.Time
	lastFail  time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attempts: map[string]*attemptState{},
		now:      time.Now,
	}
}

func (l *loginLimiter) Allow(key string) (time.Duration, bool) {
	key = limiterKey(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.attempts[key]
	if state == nil {
		return 0, true
	}
	now := l.now()
	if state.lockedTil.After(now) {
		return state.lockedTil.Sub(now), false
	}
	// 锁定已过期且长时间无失败：丢弃条目，避免计数永久累积。
	if now.Sub(state.lastFail) > failureWindow {
		delete(l.attempts, key)
	}
	return 0, true
}

func (l *loginLimiter) Fail(key string) {
	key = limiterKey(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	state := l.attempts[key]
	if state == nil {
		state = &attemptState{}
		l.attempts[key] = state
	}
	if now.Sub(state.lastFail) > failureWindow {
		state.failures = 0
	}
	state.failures++
	state.lastFail = now
	if state.failures >= lockThreshold {
		state.lockedTil = now.Add(lockDuration(state.failures))
	}
}

func (l *loginLimiter) Reset(key string) {
	key = limiterKey(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// sweep 在条目数超过上限时删除已解锁且失败计数已衰减的条目，
// 防止 attempts map 随着不同来源 IP 无限增长。
func (l *loginLimiter) sweep(now time.Time) {
	if len(l.attempts) < limiterMaxKeys {
		return
	}
	for key, state := range l.attempts {
		if state.lockedTil.After(now) {
			continue
		}
		if now.Sub(state.lastFail) > failureWindow {
			delete(l.attempts, key)
		}
	}
}

func lockDuration(failures int) time.Duration {
	if failures <= lockThreshold {
		return 5 * time.Minute
	}
	multiplier := failures - (lockThreshold - 1)
	if multiplier > 6 {
		multiplier = 6
	}
	return time.Duration(multiplier) * 5 * time.Minute
}

func limiterKey(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
