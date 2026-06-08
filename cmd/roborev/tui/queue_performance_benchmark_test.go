package tui

import (
	"fmt"
	"testing"
	"time"

	"go.kenn.io/roborev/internal/storage"
)

const benchQueueJobs = 2000

func BenchmarkQueueRenderLargeList(b *testing.B) {
	m := newBenchQueueModel(benchQueueJobs)
	_ = m.renderQueueView()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderQueueView()
	}
}

func BenchmarkQueueNavigateAndRenderLargeList(b *testing.B) {
	m := newBenchQueueModel(benchQueueJobs)
	_ = m.renderQueueView()

	dir := 1
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if m.selectedIdx <= 0 {
			dir = 1
		} else if m.selectedIdx >= len(m.jobs)-1 {
			dir = -1
		}
		m = m.moveQueueSelection(dir)
		_ = m.renderQueueView()
	}
}

func newBenchQueueModel(jobCount int) model {
	m := newModel(localhostEndpoint, withExternalIODisabled())
	m.width = 160
	m.height = 40
	m.heightDetected = true
	m.jobs = make([]storage.ReviewJob, jobCount)
	now := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)

	for i := range m.jobs {
		id := int64(jobCount - i)
		started := now.Add(time.Duration(-i) * time.Minute)
		finished := started.Add(90 * time.Second)
		closed := i%3 == 0
		verdict := "P"
		if i%5 == 0 {
			verdict = "F"
		}
		j := makeJob(id)
		j.GitRef = fmt.Sprintf("%040x", id)
		j.Branch = fmt.Sprintf("feature/queue-benchmark-%02d", i%25)
		j.RepoPath = fmt.Sprintf("/tmp/roborev-bench/repo-%02d", i%40)
		j.RepoName = fmt.Sprintf("repo-%02d", i%40)
		j.Agent = "codex"
		j.EnqueuedAt = started.Add(-30 * time.Second)
		j.StartedAt = &started
		j.FinishedAt = &finished
		j.Closed = &closed
		j.Verdict = &verdict
		j.SessionID = fmt.Sprintf("session-%04d-extra", i)
		j.RequestedModel = "gpt-5"
		j.RequestedProvider = "openai"
		if i%7 == 0 {
			j.TokenUsage = `{"cost_usd":0.42,"has_cost":true}`
		}
		m.jobs[i] = j
	}

	if len(m.jobs) > 0 {
		m.selectedIdx = 0
		m.selectedJobID = m.jobs[0].ID
	}
	m.jobStats.Done = jobCount
	m.jobStats.Closed = jobCount / 3
	m.jobStats.Open = jobCount - m.jobStats.Closed
	m.updateDisplayNameCache(m.jobs)
	return m
}
