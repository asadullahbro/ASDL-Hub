package services

import "testing"

func TestRecoveryReached(t *testing.T) {
	c := map[string]int{}
	if recoveryReached(c, "a", true) {
		t.Fatal("one good check is not enough")
	}
	if !recoveryReached(c, "a", true) {
		t.Fatal("two good checks in a row should recover")
	}
	if len(c) != 0 {
		t.Fatal("counter should be cleared after recovery")
	}
	recoveryReached(c, "a", true)
	if recoveryReached(c, "a", false) || recoveryReached(c, "a", true) {
		t.Fatal("a bad check must reset the streak")
	}
	if !recoveryReached(c, "a", true) {
		t.Fatal("two good checks after the reset should recover")
	}
}
