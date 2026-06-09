package server

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterLocksAfterThreshold(t *testing.T) {
	l := newLoginLimiter()
	base := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return base }

	for i := 0; i < lockThreshold-1; i++ {
		l.Fail("1.2.3.4")
		if _, ok := l.Allow("1.2.3.4"); !ok {
			t.Fatalf("locked after %d failures, want allowed until %d", i+1, lockThreshold)
		}
	}

	l.Fail("1.2.3.4")
	retryAfter, ok := l.Allow("1.2.3.4")
	if ok {
		t.Fatal("expected lockout after reaching threshold")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive", retryAfter)
	}

	// A different IP must be unaffected.
	if _, ok := l.Allow("5.6.7.8"); !ok {
		t.Fatal("unrelated IP should not be locked")
	}
}

func TestLoginLimiterEscalatesLockDuration(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < lockThreshold; i++ {
		l.Fail("1.1.1.1")
	}
	first, ok := l.Allow("1.1.1.1")
	if ok {
		t.Fatal("expected lockout")
	}

	// Lock expires but we are still inside the failure window: the next
	// failure must escalate rather than reset.
	now = now.Add(first + time.Second)
	if _, ok := l.Allow("1.1.1.1"); !ok {
		t.Fatal("lock should have expired")
	}
	l.Fail("1.1.1.1")
	second, ok := l.Allow("1.1.1.1")
	if ok {
		t.Fatal("expected re-lock after escalation")
	}
	if second <= first {
		t.Fatalf("escalated lock = %v, want longer than %v", second, first)
	}
}

func TestLoginLimiterDecaysAfterWindow(t *testing.T) {
	l := newLoginLimiter()
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }

	for i := 0; i < lockThreshold-1; i++ {
		l.Fail("9.9.9.9")
	}
	if l.attempts["9.9.9.9"].failures != lockThreshold-1 {
		t.Fatalf("failures = %d, want %d", l.attempts["9.9.9.9"].failures, lockThreshold-1)
	}

	// Idle past the failure window: Allow should drop the stale entry.
	now = now.Add(failureWindow + time.Minute)
	if _, ok := l.Allow("9.9.9.9"); !ok {
		t.Fatal("decayed entry should be allowed")
	}
	if _, exists := l.attempts["9.9.9.9"]; exists {
		t.Fatal("stale entry should have been removed on Allow")
	}

	// A failure after decay starts a fresh count.
	l.Fail("9.9.9.9")
	if got := l.attempts["9.9.9.9"].failures; got != 1 {
		t.Fatalf("post-decay failures = %d, want 1", got)
	}
}

func TestLoginLimiterSweepBoundsMemory(t *testing.T) {
	l := newLoginLimiter()
	base := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return base }

	// Fill with stale, unlocked entries up to the sweep trigger.
	for i := 0; i < limiterMaxKeys; i++ {
		l.attempts[fmt.Sprintf("ip-%d", i)] = &attemptState{failures: 1, lastFail: base.Add(-time.Hour)}
	}

	// The next failure triggers a sweep that removes all stale entries.
	l.Fail("fresh")
	if len(l.attempts) != 1 {
		t.Fatalf("attempts size = %d, want 1 after sweep", len(l.attempts))
	}
	if _, ok := l.attempts["fresh"]; !ok {
		t.Fatal("the fresh entry should survive the sweep")
	}
}
