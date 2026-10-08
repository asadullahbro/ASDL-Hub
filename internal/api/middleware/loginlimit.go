package middleware

import (
	"strings"
	"sync"
	"time"
)

// Failed logins allowed per window before more attempts are refused.
// The limits are per client address and, higher, per username: the address
// limit stops one machine guessing, the username one stops many machines
// guessing at one account.
const (
	loginWindow          = 15 * time.Minute
	loginFailuresPerIP   = 8
	loginFailuresPerUser = 20
	limiterMaxKeys       = 10000
)

// LoginLimiter counts failed logins and says when to stop accepting more.
// It lives in memory, so a Hub restart clears it.
type LoginLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	failures map[string][]time.Time
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{now: time.Now, failures: map[string][]time.Time{}}
}

func ipKey(ip string) string     { return "ip:" + ip }
func userKey(name string) string { return "user:" + strings.ToLower(strings.TrimSpace(name)) }
func (l *LoginLimiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-loginWindow)
	times := l.failures[key]
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	if i > 0 {
		times = times[i:]
		if len(times) == 0 {
			delete(l.failures, key)
		} else {
			l.failures[key] = times
		}
	}
	return times
}

// Blocked reports whether a login from this address for this username should
// be refused, and how long until it may try again.
func (l *LoginLimiter) Blocked(ip, username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, c := range []struct {
		key string
		max int
	}{{ipKey(ip), loginFailuresPerIP}, {userKey(username), loginFailuresPerUser}} {
		times := l.recent(c.key)
		if len(times) >= c.max {
			if w := times[len(times)-c.max].Add(loginWindow).Sub(l.now()); w > wait {
				wait = w
			}
		}
	}
	return wait > 0, wait
}

// Failed records a wrong password.
func (l *LoginLimiter) Failed(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) > limiterMaxKeys {
		// Bound memory against someone inventing usernames: drop what has expired.
		for k := range l.failures {
			l.recent(k)
		}
	}
	now := l.now()
	for _, k := range []string{ipKey(ip), userKey(username)} {
		l.failures[k] = append(l.recent(k), now)
	}
}

// Succeeded clears the username's count. The address's count stays, so
// logging in to an account you own doesn't buy more guesses at others.
func (l *LoginLimiter) Succeeded(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, userKey(username))
}
