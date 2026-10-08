package middleware

import (
	"testing"
	"time"
)

func limiterAt(now *time.Time) *LoginLimiter {
	l := NewLoginLimiter()
	l.now = func() time.Time { return *now }
	return l
}

func TestLoginLimiter_BlocksOneAddressAfterTooManyFailures(t *testing.T) {
	now := time.Now()
	l := limiterAt(&now)
	for i := 0; i < loginFailuresPerIP; i++ {
		if blocked, _ := l.Blocked("203.0.113.9", "user"+string(rune('a'+i))); blocked {
			t.Fatalf("blocked after only %d failures", i)
		}
		// A different username each time: only the address is repeating.
		l.Failed("203.0.113.9", "user"+string(rune('a'+i)))
	}
	blocked, wait := l.Blocked("203.0.113.9", "someone-new")
	if !blocked || wait <= 0 || wait > loginWindow {
		t.Fatalf("expected a block of at most %v, got blocked=%v wait=%v", loginWindow, blocked, wait)
	}
	if blocked, _ := l.Blocked("198.51.100.1", "someone-new"); blocked {
		t.Error("another address must not be affected")
	}
}

func TestLoginLimiter_BlocksOneAccountFromManyAddresses(t *testing.T) {
	now := time.Now()
	l := limiterAt(&now)
	for i := 0; i < loginFailuresPerUser; i++ {
		l.Failed("10.0.0."+string(rune('a'+i%20)), "Admin")
	}
	// The name is matched without regard to case or spaces.
	if blocked, _ := l.Blocked("192.0.2.1", " admin "); !blocked {
		t.Error("an account under attack from many addresses should be locked for new ones too")
	}
}

func TestLoginLimiter_ForgetsAfterTheWindow(t *testing.T) {
	now := time.Now()
	l := limiterAt(&now)
	for i := 0; i < loginFailuresPerIP; i++ {
		l.Failed("203.0.113.9", "alice")
	}
	if blocked, _ := l.Blocked("203.0.113.9", "alice"); !blocked {
		t.Fatal("should be blocked")
	}
	now = now.Add(loginWindow + time.Second)
	if blocked, _ := l.Blocked("203.0.113.9", "alice"); blocked {
		t.Error("the block should end once the failures are older than the window")
	}
}

func TestLoginLimiter_SuccessClearsTheAccountNotTheAddress(t *testing.T) {
	now := time.Now()
	l := limiterAt(&now)
	for i := 0; i < loginFailuresPerIP-1; i++ {
		l.Failed("203.0.113.9", "alice")
	}
	l.Succeeded("alice")
	l.Failed("203.0.113.9", "bob") // the address is now at its limit
	if blocked, _ := l.Blocked("203.0.113.9", "carol"); !blocked {
		t.Error("a success must not give the address its failures back")
	}
	if blocked, _ := l.Blocked("198.51.100.1", "alice"); blocked {
		t.Error("a success should clear the account's own count")
	}
}
