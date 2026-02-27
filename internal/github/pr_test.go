package github

import (
	"encoding/json"
	"testing"
)

func TestRollupStatus_AllPass(t *testing.T) {
	checks := []ghCheck{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}
	if got := rollupStatus(checks); got != CheckSuccess {
		t.Errorf("rollupStatus(all pass) = %d, want CheckSuccess(%d)", got, CheckSuccess)
	}
}

func TestRollupStatus_SomeFail(t *testing.T) {
	checks := []ghCheck{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Status: "COMPLETED", Conclusion: "FAILURE"},
	}
	if got := rollupStatus(checks); got != CheckFailure {
		t.Errorf("rollupStatus(some fail) = %d, want CheckFailure(%d)", got, CheckFailure)
	}
}

func TestRollupStatus_SomePending(t *testing.T) {
	checks := []ghCheck{
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Status: "IN_PROGRESS", Conclusion: ""},
	}
	if got := rollupStatus(checks); got != CheckPending {
		t.Errorf("rollupStatus(some pending) = %d, want CheckPending(%d)", got, CheckPending)
	}
}

func TestRollupStatus_Empty(t *testing.T) {
	if got := rollupStatus(nil); got != CheckUnknown {
		t.Errorf("rollupStatus(nil) = %d, want CheckUnknown(%d)", got, CheckUnknown)
	}
}

func TestRollupStatus_SkippedAndNeutral(t *testing.T) {
	checks := []ghCheck{
		{Status: "COMPLETED", Conclusion: "NEUTRAL"},
		{Status: "COMPLETED", Conclusion: "SKIPPED"},
		{Status: "COMPLETED", Conclusion: "SUCCESS"},
	}
	if got := rollupStatus(checks); got != CheckSuccess {
		t.Errorf("rollupStatus(neutral+skipped+success) = %d, want CheckSuccess(%d)", got, CheckSuccess)
	}
}

func TestRollupStatus_FailureBeatsProgress(t *testing.T) {
	checks := []ghCheck{
		{Status: "IN_PROGRESS", Conclusion: ""},
		{Status: "COMPLETED", Conclusion: "FAILURE"},
	}
	if got := rollupStatus(checks); got != CheckFailure {
		t.Errorf("rollupStatus(pending+failure) = %d, want CheckFailure(%d)", got, CheckFailure)
	}
}

func TestGHPRViewJSON(t *testing.T) {
	// Simulate the JSON output from `gh pr view --json`
	raw := `{
		"number": 42,
		"url": "https://github.com/owner/repo/pull/42",
		"state": "OPEN",
		"statusCheckRollup": [
			{"status": "COMPLETED", "conclusion": "SUCCESS"},
			{"status": "IN_PROGRESS", "conclusion": ""}
		]
	}`

	var view ghPRView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if view.Number != 42 {
		t.Errorf("Number = %d, want 42", view.Number)
	}
	if view.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q, want github PR URL", view.URL)
	}
	if view.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", view.State)
	}
	if got := rollupStatus(view.StatusCheckRollup); got != CheckPending {
		t.Errorf("rollupStatus = %d, want CheckPending(%d)", got, CheckPending)
	}
}

func TestGHPRViewJSON_Merged(t *testing.T) {
	raw := `{
		"number": 99,
		"url": "https://github.com/owner/repo/pull/99",
		"state": "MERGED",
		"statusCheckRollup": [
			{"status": "COMPLETED", "conclusion": "SUCCESS"}
		]
	}`

	var view ghPRView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if view.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", view.State)
	}
}
