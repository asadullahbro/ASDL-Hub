package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Repo is a minimal representation of a GitHub repository.
// Only fields ASDL Hub actually needs — nothing extra.
type Repo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Owner         string `json:"-"` // populated from owner.login below
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
}

type repoResponse struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// ListRepositories returns all repositories accessible to a given installation.
// Handles GitHub's pagination automatically.
func (a *AppClient) ListRepositories(installationID int64) ([]Repo, error) {
	token, err := a.InstallationToken(installationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	var all []Repo
	page := 1

	for {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", githubAPIBase, page)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list repositories request failed: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			TotalCount   int            `json:"total_count"`
			Repositories []repoResponse `json:"repositories"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse repositories response: %w", err)
		}

		for _, r := range result.Repositories {
			all = append(all, Repo{
				ID:            r.ID,
				Name:          r.Name,
				FullName:      r.FullName,
				Owner:         r.Owner.Login,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
				HTMLURL:       r.HTMLURL,
				CloneURL:      r.CloneURL,
			})
		}

		// Stop if we got all repos
		if len(all) >= result.TotalCount || len(result.Repositories) == 0 {
			break
		}
		page++
	}

	return all, nil
}

// GetRepository fetches a single repository by owner and name.
// Used to verify access and fetch metadata when linking a repo to a project.
func (a *AppClient) GetRepository(installationID int64, owner, name string) (*Repo, error) {
	token, err := a.InstallationToken(installationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, owner, name)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get repository request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, string(body))
	}

	var r repoResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("failed to parse repository response: %w", err)
	}

	return &Repo{
		ID:            r.ID,
		Name:          r.Name,
		FullName:      r.FullName,
		Owner:         r.Owner.Login,
		DefaultBranch: r.DefaultBranch,
		Private:       r.Private,
		HTMLURL:       r.HTMLURL,
		CloneURL:      r.CloneURL,
	}, nil
}
