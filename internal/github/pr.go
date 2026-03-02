package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// CheckStatus represents the aggregate CI check status of a PR.
type CheckStatus int

const (
	CheckUnknown CheckStatus = iota // No checks or unable to determine.
	CheckPending                    // At least one check still running, none failed.
	CheckSuccess                    // All checks passed.
	CheckFailure                    // At least one check failed.
	CheckMerged                     // PR has been merged.
)

// PRInfo holds essential info about a GitHub pull request.
type PRInfo struct {
	Number            int
	URL               string
	Status            CheckStatus
	IsDraft           bool // true when PR is a draft
	Conflicting       bool // true when PR has merge conflicts with base branch
	UnresolvedThreads int  // count of unresolved review threads (-1 = unknown)
}

// GHInstalled returns true if the gh CLI is on PATH.
func GHInstalled() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ghPRView is the subset of `gh pr view --json` output we care about.
type ghPRView struct {
	Number            int       `json:"number"`
	URL               string    `json:"url"`
	State             string    `json:"state"`
	IsDraft           bool      `json:"isDraft"`
	Mergeable         string    `json:"mergeable"` // MERGEABLE, CONFLICTING, UNKNOWN
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Status     string `json:"status"`     // e.g. "COMPLETED", "IN_PROGRESS", "QUEUED"
	Conclusion string `json:"conclusion"` // e.g. "SUCCESS", "FAILURE", "NEUTRAL", "SKIPPED"
	State      string `json:"state"`      // alternative field used by some check types
}

// FetchPRForBranch looks up an open/merged PR for the given branch using `gh pr view`.
// Returns nil, nil when no PR exists for the branch.
// repoRoot should be a path inside the git repo so gh can detect the remote.
func FetchPRForBranch(ctx context.Context, branch, repoRoot string) (*PRInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "pr", "view", branch,
		"--json", "number,url,state,isDraft,mergeable,statusCheckRollup")
	cmd.Dir = repoRoot

	out, err := cmd.Output()
	if err != nil {
		// gh exits 1 when no PR exists — treat as "no PR".
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	var view ghPRView
	if err := json.Unmarshal(out, &view); err != nil {
		return nil, err
	}

	info := &PRInfo{
		Number:      view.Number,
		URL:         view.URL,
		IsDraft:     view.IsDraft,
		Conflicting: view.Mergeable == "CONFLICTING",
	}

	if view.State == "MERGED" {
		info.Status = CheckMerged
	} else {
		info.Status = rollupStatus(view.StatusCheckRollup)
	}

	return info, nil
}

// rollupStatus computes an aggregate CheckStatus from individual check results.
func rollupStatus(checks []ghCheck) CheckStatus {
	if len(checks) == 0 {
		return CheckUnknown
	}

	hasPending := false
	for _, c := range checks {
		// Normalize: some checks use State, some use Conclusion.
		conclusion := c.Conclusion
		if conclusion == "" {
			conclusion = c.State
		}
		status := c.Status

		switch {
		case conclusion == "FAILURE" || conclusion == "ERROR" ||
			conclusion == "TIMED_OUT" || conclusion == "CANCELLED" ||
			conclusion == "ACTION_REQUIRED":
			return CheckFailure
		case status == "IN_PROGRESS" || status == "QUEUED" ||
			conclusion == "PENDING" || status == "PENDING":
			hasPending = true
		}
		// SUCCESS, NEUTRAL, SKIPPED, STALE — all fine.
	}

	if hasPending {
		return CheckPending
	}
	return CheckSuccess
}

// parseOwnerRepo extracts the owner and repo from a GitHub PR URL
// like "https://github.com/owner/repo/pull/123".
func parseOwnerRepo(prURL string) (owner, repo string, ok bool) {
	u, err := url.Parse(prURL)
	if err != nil || u.Host != "github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// graphqlResponse is used to unmarshal the reviewThreads query.
type graphqlResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// FetchUnresolvedThreads returns the number of unresolved review threads for a PR.
// Returns -1 on error (caller can treat as "unknown").
func FetchUnresolvedThreads(ctx context.Context, prURL string, prNumber int) int {
	owner, repo, ok := parseOwnerRepo(prURL)
	if !ok {
		return -1
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := fmt.Sprintf(`query { repository(owner: %q, name: %q) { pullRequest(number: %d) { reviewThreads(first: 100) { nodes { isResolved } } } } }`, owner, repo, prNumber)

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql", "-f", "query="+query)
	out, err := cmd.Output()
	if err != nil {
		return -1
	}

	var resp graphqlResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return -1
	}

	count := 0
	for _, t := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !t.IsResolved {
			count++
		}
	}
	return count
}
