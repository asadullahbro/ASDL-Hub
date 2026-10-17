package services

import (
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
)

func TestContainerConfirmedDown(t *testing.T) {
	now := time.Now()
	p := &models.Project{ID: "p1", Name: "gamer-ak-bot"}
	fresh := now.Add(-10 * time.Second)
	node := func(beat time.Time, cs ...models.NodeContainer) *models.Node {
		return &models.Node{LastHeartbeat: beat, Containers: cs}
	}
	other := models.NodeContainer{Name: "redis", State: "running"}

	cases := []struct {
		name string
		n    *models.Node
		want bool
	}{
		{"exited container: really down", node(fresh, other, models.NodeContainer{Name: "gamer-ak-bot", State: "exited"}), true},
		{"matched by project id even if renamed", node(fresh, other, models.NodeContainer{Name: "x", ProjectID: "p1", State: "exited"}), true},
		{"container gone from the report", node(fresh, other), true},
		{"restarting counts as not running", node(fresh, other, models.NodeContainer{Name: "gamer-ak-bot", State: "restarting"}), true},
		{"running per the agent: a network blip or a hang, not a crash", node(fresh, other, models.NodeContainer{Name: "gamer-ak-bot", State: "running"}), false},
		{"stale report cannot be trusted", node(now.Add(-5*time.Minute), other), false},
		{"agent reports nothing (old agent): no answer", node(fresh), false},
		{"never reported", node(time.Time{}, other), false},
	}
	for _, c := range cases {
		if got := containerConfirmedDown(c.n, p, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
