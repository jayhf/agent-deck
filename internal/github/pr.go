package github

import (
	"context"
	"encoding/json"
	"os/exec"
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
	Number int
	URL    string
	Status CheckStatus
}

// GHInstalled returns true if the gh CLI is on PATH.
func GHInstalled() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ghPRView is the subset of `gh pr view --json` output we care about.
type ghPRView struct {
	Number             int       `json:"number"`
	URL                string    `json:"url"`
	State              string    `json:"state"`
	StatusCheckRollup  []ghCheck `json:"statusCheckRollup"`
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
		"--json", "number,url,state,statusCheckRollup")
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
		Number: view.Number,
		URL:    view.URL,
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
