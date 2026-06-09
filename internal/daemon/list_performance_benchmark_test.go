package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.kenn.io/roborev/internal/config"
	"go.kenn.io/roborev/internal/storage"
)

const (
	benchReviewJobs = 2000
	benchOutputSize = 16 * 1024
)

type listBenchFixture struct {
	db     *storage.DB
	server *Server
}

func BenchmarkListJobsHandler(b *testing.B) {
	f := newListBenchFixture(b)

	b.Run("limit_100", func(b *testing.B) {
		input := &ListJobsInput{
			ID:               -1,
			Limit:            100,
			Offset:           -1,
			Before:           -1,
			ExcludeJobType:   storage.JobTypeFix,
			HideClassifyJobs: "true",
			Closed:           "false",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := f.server.humaListJobs(context.Background(), input)
			if err != nil {
				b.Fatalf("humaListJobs: %v", err)
			}
			if got := len(resp.Body.Jobs); got != 100 {
				b.Fatalf("jobs length = %d, want 100", got)
			}
		}
	})

	b.Run("unlimited", func(b *testing.B) {
		input := &ListJobsInput{
			ID:               -1,
			Limit:            0,
			Offset:           -1,
			Before:           -1,
			ExcludeJobType:   storage.JobTypeFix,
			HideClassifyJobs: "true",
			Closed:           "false",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := f.server.humaListJobs(context.Background(), input)
			if err != nil {
				b.Fatalf("humaListJobs: %v", err)
			}
			if got := len(resp.Body.Jobs); got != benchReviewJobs {
				b.Fatalf("jobs length = %d, want %d", got, benchReviewJobs)
			}
		}
	})
}

func BenchmarkListJobsStorage(b *testing.B) {
	f := newListBenchFixture(b)
	opts := []storage.ListJobsOption{
		storage.WithExcludeJobType(storage.JobTypeFix),
		storage.WithHideClassifyJobs(),
		storage.WithClosed(false),
	}

	b.Run("limit_100", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			jobs, err := f.db.ListJobs("", "", 100, 0, opts...)
			if err != nil {
				b.Fatalf("ListJobs: %v", err)
			}
			if got := len(jobs); got != 100 {
				b.Fatalf("jobs length = %d, want 100", got)
			}
		}
	})

	b.Run("unlimited", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			jobs, err := f.db.ListJobs("", "", 0, 0, opts...)
			if err != nil {
				b.Fatalf("ListJobs: %v", err)
			}
			if got := len(jobs); got != benchReviewJobs {
				b.Fatalf("jobs length = %d, want %d", got, benchReviewJobs)
			}
		}
	})
}

func newListBenchFixture(b *testing.B) listBenchFixture {
	b.Helper()

	db, err := storage.Open(b.TempDir() + "/reviews.db")
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	b.Cleanup(func() {
		if err := db.Close(); err != nil {
			b.Fatalf("close db: %v", err)
		}
	})

	repo, err := db.GetOrCreateRepo("/tmp/roborev-list-bench-repo")
	if err != nil {
		b.Fatalf("create repo: %v", err)
	}

	seedListBenchRows(b, db, repo.ID)

	return listBenchFixture{
		db:     db,
		server: NewServer(db, config.DefaultConfig(), ""),
	}
}

func seedListBenchRows(b *testing.B, db *storage.DB, repoID int64) {
	b.Helper()

	output := benchOutput(benchOutputSize)
	tokenUsage := `{"total_output_tokens":28800,"peak_context_tokens":118000,"cost_usd":0.42,"has_cost":true}`
	now := time.Now().UTC()

	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("begin seed transaction: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	commitStmt, err := tx.Prepare(`
		INSERT INTO commits (repo_id, sha, author, subject, timestamp)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		b.Fatalf("prepare commits: %v", err)
	}
	defer commitStmt.Close()

	jobStmt, err := tx.Prepare(`
		INSERT INTO review_jobs
			(repo_id, commit_id, git_ref, branch, session_id, agent, reasoning,
			 status, enqueued_at, started_at, finished_at, prompt, job_type,
			 review_type, token_usage, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'done', ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		b.Fatalf("prepare jobs: %v", err)
	}
	defer jobStmt.Close()

	reviewStmt, err := tx.Prepare(`
		INSERT INTO reviews (job_id, agent, prompt, output, closed, verdict_bool)
		VALUES (?, ?, ?, ?, 0, 0)
	`)
	if err != nil {
		b.Fatalf("prepare reviews: %v", err)
	}
	defer reviewStmt.Close()

	for i := 0; i < benchReviewJobs; i++ {
		sha := fmt.Sprintf("%040x", i+1)
		subject := fmt.Sprintf("benchmark commit %04d", i)
		ts := now.Add(time.Duration(-i) * time.Minute).Format(time.RFC3339)

		res, err := commitStmt.Exec(repoID, sha, "Benchmark User", subject, ts)
		if err != nil {
			b.Fatalf("insert commit %d: %v", i, err)
		}
		commitID, err := res.LastInsertId()
		if err != nil {
			b.Fatalf("commit id %d: %v", i, err)
		}

		res, err = jobStmt.Exec(
			repoID, commitID, sha, "main",
			fmt.Sprintf("session-%04d", i), "codex", "thorough",
			ts, ts, ts, "benchmark prompt", storage.JobTypeReview,
			"", tokenUsage, "",
		)
		if err != nil {
			b.Fatalf("insert job %d: %v", i, err)
		}
		jobID, err := res.LastInsertId()
		if err != nil {
			b.Fatalf("job id %d: %v", i, err)
		}
		if _, err := reviewStmt.Exec(jobID, "codex", "benchmark prompt", output); err != nil {
			b.Fatalf("insert review %d: %v", i, err)
		}
	}

	for i := 0; i < 25; i++ {
		if _, err := jobStmt.Exec(
			repoID, nil, fmt.Sprintf("fix-%04d", i), "main",
			"", "codex", "standard",
			now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339),
			"fix prompt", storage.JobTypeFix, "", "", "",
		); err != nil {
			b.Fatalf("insert excluded fix job %d: %v", i, err)
		}
		if _, err := jobStmt.Exec(
			repoID, nil, fmt.Sprintf("classify-%04d", i), "main",
			"", "codex", "fast",
			now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339),
			"classify prompt", storage.JobTypeClassify, "design", "", "auto_design",
		); err != nil {
			b.Fatalf("insert hidden classify job %d: %v", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed transaction: %v", err)
	}
	committed = true
}

func benchOutput(size int) string {
	line := "- Medium - synthetic benchmark finding with enough detail to resemble a review output.\n"
	var b strings.Builder
	for b.Len() < size {
		b.WriteString(line)
	}
	return b.String()
}
