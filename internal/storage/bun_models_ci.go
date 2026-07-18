package storage

import "github.com/uptrace/bun"

type ciPRReviewRow struct { //nolint:unused // Consumed by the staged Bun query conversion.
	bun.BaseModel `bun:"table:ci_pr_reviews,alias:cpr"`
	ID            int64  `bun:"id,pk,autoincrement"`
	GithubRepo    string `bun:"github_repo"`
	PRNumber      int    `bun:"pr_number"`
	HeadSHA       string `bun:"head_sha"`
	JobID         int64  `bun:"job_id"`
	CreatedAt     dbTime `bun:"created_at"`
}

type ciPanelRow struct { //nolint:unused // Consumed by the staged Bun query conversion.
	bun.BaseModel    `bun:"table:ci_pr_panels,alias:cp"`
	ID               int64   `bun:"id,pk,autoincrement"`
	GithubRepo       string  `bun:"github_repo"`
	PRNumber         int     `bun:"pr_number"`
	HeadSHA          string  `bun:"head_sha"`
	PanelRunUUID     string  `bun:"panel_run_uuid"`
	SynthesisJobID   *int64  `bun:"synthesis_job_id"`
	CreatedAt        dbTime  `bun:"created_at"`
	PostingClaimedAt dbTime  `bun:"posting_claimed_at"`
	PostedAt         dbTime  `bun:"posted_at"`
	RetiredAt        dbTime  `bun:"retired_at"`
	Outcome          *string `bun:"outcome"`
	FirstAttemptAt   dbTime  `bun:"first_attempt_at"`
	AttemptCount     *int64  `bun:"attempt_count"`
	SynthesisAgent   *string `bun:"synthesis_agent"`
	SynthesisModel   *string `bun:"synthesis_model"`
	AllowStalePost   bool    `bun:"allow_stale_post"`
}

type ciReviewAttemptRow struct { //nolint:unused // Consumed by the staged Bun query conversion.
	bun.BaseModel              `bun:"table:ci_pr_review_attempts,alias:ca"`
	ID                         int64  `bun:"id,pk,autoincrement"`
	GithubRepo                 string `bun:"github_repo"`
	PRNumber                   int    `bun:"pr_number"`
	HeadSHA                    string `bun:"head_sha"`
	Attempt                    int    `bun:"attempt"`
	FirstAttemptAt             dbTime `bun:"first_attempt_at"`
	NextAttemptAt              dbTime `bun:"next_attempt_at"`
	LastErrorClass             string `bun:"last_error_class"`
	ConsecutiveGenuineAttempts int    `bun:"consecutive_genuine_attempts"`
	LastErrorExcerpt           string `bun:"last_error_excerpt"`
	LastPanelRunUUID           string `bun:"last_panel_run_uuid"`
	State                      string `bun:"state"`
	UpdatedAt                  dbTime `bun:"updated_at"`
}

type daemonStateRow struct {
	bun.BaseModel `bun:"table:daemon_state,alias:ds"`
	Key           string `bun:"key,pk"`
	Value         string `bun:"value"`
	UpdatedAt     dbTime `bun:"updated_at"`
}

type syncStateRow struct {
	bun.BaseModel `bun:"table:sync_state,alias:ss"`
	Key           string `bun:"key,pk"`
	Value         string `bun:"value"`
}
