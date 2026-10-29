package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// githubAPIURL is where repository licenses are read; a test points it at a
// fake.
var githubAPIURL = "https://api.github.com"

// repoLicense is the repository's license at a tag, as GitHub detects it
// from its LICENSE file: an SPDX id ("MIT"), or "" when there's none, GitHub
// can't tell (NOASSERTION), or the lookup fails. A failure never blocks a
// registration; it's returned for the log.
func repoLicense(ctx context.Context, sourceUrl, tag, authToken string) (string, error) {
	owner, repo, ok := ownerRepo(sourceUrl)
	if !ok {
		return "", fmt.Errorf("not a GitHub repository URL: %s", sourceUrl)
	}
	u := fmt.Sprintf("%s/repos/%s/%s/license", githubAPIURL, url.PathEscape(owner), url.PathEscape(repo))
	if tag != "" {
		u += "?ref=" + url.QueryEscape(tag)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // no LICENSE file
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %d for the license of %s/%s", resp.StatusCode, owner, repo)
	}
	var body struct {
		License struct {
			SpdxID string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("reading GitHub's license answer: %w", err)
	}
	id := strings.TrimSpace(body.License.SpdxID)
	if id == "NOASSERTION" {
		return "", nil
	}
	return id, nil
}

// ownerRepo reads owner and repository from a GitHub URL
// (https://github.com/owner/repo, with or without .git or a trailing slash).
func ownerRepo(sourceUrl string) (string, string, bool) {
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(sourceUrl), "/"), ".git")
	i := strings.Index(s, "github.com/")
	if i < 0 {
		return "", "", false
	}
	parts := strings.Split(s[i+len("github.com/"):], "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
