package handlers

import "testing"

func TestImageBelongsToRepo(t *testing.T) {
	cases := []struct {
		image, repo string
		ok          bool
	}{
		{"ghcr.io/asadullahbro/simplebanking-backend:4b91165", "asadullahbro/SimpleBanking-backend", true},
		{"ghcr.io/asadullahbro/simplebanking-backend", "asadullahbro/SimpleBanking-backend", true},
		{"ghcr.io/asadullahbro/simplebanking-backend/api:v1", "asadullahbro/SimpleBanking-backend", true},
		{"ghcr.io/asadullahbro/simplebanking-backend@sha256:abc", "asadullahbro/SimpleBanking-backend", true},
		{"ghcr.io/asadullahbro/simplebanking-backend-evil:v1", "asadullahbro/SimpleBanking-backend", false},
		{"ghcr.io/someoneelse/tool:v1", "asadullahbro/SimpleBanking-backend", false},
		{"docker.io/library/nginx:latest", "asadullahbro/SimpleBanking-backend", false},
		{"ghcr.io/asadullahbro/simplebanking-backend:v1", "", false},
	}
	for _, tc := range cases {
		if got := imageBelongsToRepo(tc.image, tc.repo); got != tc.ok {
			t.Errorf("imageBelongsToRepo(%q, %q) = %v, want %v", tc.image, tc.repo, got, tc.ok)
		}
	}
}
