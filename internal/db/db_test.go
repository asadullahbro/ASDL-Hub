package db

import "testing"

func TestMaxConns(t *testing.T) {
	cases := []struct{ env, want string }{{"", "40"}, {"25", "25"}, {"0", "40"}, {"-3", "40"}, {"lots", "40"}}
	for _, c := range cases {
		t.Setenv("DB_MAX_CONNS", c.env)
		if got := maxConns(); got < 1 || (c.want == "40" && got != 40) || (c.want == "25" && got != 25) {
			t.Errorf("DB_MAX_CONNS=%q gave %d, want %s", c.env, got, c.want)
		}
	}
}
