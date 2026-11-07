package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func nodeSetServer(t *testing.T, agentParallel bool) (sent *map[string]int) {
	t.Helper()
	sent = &map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/nodes":
			json.NewEncoder(w).Encode([]map[string]interface{}{{"id": "n1", "hostname": "laptop", "online": true, "agent_version": "v1"}})
		case r.URL.Path == "/api/v1/nodes/n1/parallel-jobs" && r.Method == http.MethodPut:
			json.NewDecoder(r.Body).Decode(sent)
			n := (*sent)["parallel_jobs"]
			if n == 0 {
				n = 3 // the Hub fills in its recommendation
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"parallel_jobs": n, "recommended_parallel_jobs": 3, "agent_supports_parallel": agentParallel})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ASDL_HUB_URL", srv.URL)
	t.Setenv("ASDL_HUB_TOKEN", "t")
	return sent
}

func TestNodeSet_ParallelJobs(t *testing.T) {
	sent := nodeSetServer(t, true)
	c, _ := newClient()
	var out bytes.Buffer
	if err := (&env{out: &out}).nodeSet(c, "laptop", []string{"parallel-jobs=2"}); err != nil {
		t.Fatal(err)
	}
	if (*sent)["parallel_jobs"] != 2 || !strings.Contains(out.String(), "laptop now runs 2 jobs at once (recommended for its hardware: 3)") {
		t.Fatalf("sent %v, output %q", *sent, out.String())
	}

	out.Reset()
	if err := (&env{out: &out}).nodeSet(c, "laptop", []string{"parallel_jobs=auto"}); err != nil {
		t.Fatal(err)
	}
	if (*sent)["parallel_jobs"] != 0 || !strings.Contains(out.String(), "3 jobs at once") {
		t.Fatalf("auto sent %v, output %q", *sent, out.String())
	}
}

func TestNodeSet_WarnsAboutAnOldAgent(t *testing.T) {
	nodeSetServer(t, false)
	c, _ := newClient()
	var out bytes.Buffer
	if err := (&env{out: &out}).nodeSet(c, "laptop", []string{"parallel-jobs=4"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "runs one job at a time until it is updated") {
		t.Fatalf("no old-agent warning: %q", out.String())
	}
}

func TestNodeSet_RejectsBadValues(t *testing.T) {
	nodeSetServer(t, true)
	c, _ := newClient()
	for _, bad := range []string{"parallel-jobs=0", "parallel-jobs=9", "parallel-jobs=many", "colour=blue", "parallel-jobs"} {
		if err := (&env{out: &bytes.Buffer{}}).nodeSet(c, "laptop", []string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestJobsCell(t *testing.T) {
	cases := []struct {
		n    node
		want string
	}{
		{node{ParallelJobs: 3, Capabilities: []string{"parallel_jobs"}}, "3"},
		{node{RecommendedParallelJobs: 2, Capabilities: []string{"parallel_jobs"}}, "2"},
		{node{ParallelJobs: 3, Capabilities: []string{"docker"}}, "1 (old agent)"},
		{node{}, "-"},
	}
	for _, c := range cases {
		if got := jobsCell(c.n); got != c.want {
			t.Errorf("jobsCell(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
}
