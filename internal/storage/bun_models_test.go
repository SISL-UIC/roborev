package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBTimeScan(t *testing.T) {
	want := time.Date(2026, time.July, 18, 15, 4, 5, 123456789, time.UTC)

	for _, tt := range []struct {
		name      string
		value     any
		want      time.Time
		wantValid bool
	}{
		{name: "native time", value: want, want: want, wantValid: true},
		{name: "RFC3339 string", value: want.Format(time.RFC3339Nano), want: want, wantValid: true},
		{name: "RFC3339 bytes", value: []byte(want.Format(time.RFC3339Nano)), want: want, wantValid: true},
		{
			name:      "SQLite datetime",
			value:     "2026-07-18 15:04:05",
			want:      time.Date(2026, time.July, 18, 15, 4, 5, 0, time.UTC),
			wantValid: true,
		},
		{
			name:      "SQLite time value",
			value:     "2026-07-18 15:04:05.123456789+00:00",
			want:      want,
			wantValid: true,
		},
		{name: "null", value: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got dbTime
			require.NoError(t, got.Scan(tt.value))
			assert.Equal(t, tt.wantValid, got.Valid)
			assert.True(t, got.Time.Equal(tt.want))
		})
	}
}

func TestDBTimeValue(t *testing.T) {
	want := time.Date(2026, time.July, 18, 15, 4, 5, 123456789, time.UTC)

	value, err := (dbTime{Time: want, Valid: true}).Value()
	require.NoError(t, err)
	assert.Equal(t, want.Format(time.RFC3339Nano), value)

	value, err = (dbTime{}).Value()
	require.NoError(t, err)
	assert.Nil(t, value)

	assert.True(t, dbTimeFromValue(time.Time{}).Valid)
}

func TestReviewJobRowRoundTripPreservesPersistedFields(t *testing.T) {
	commitID := int64(12)
	parentJobID := int64(8)
	diffContent := "diff --git a/a.go b/a.go"
	patch := "diff --git a/fixed.go b/fixed.go"
	startedAt := time.Date(2026, time.July, 18, 15, 5, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.July, 18, 15, 6, 0, 0, time.UTC)

	want := ReviewJob{
		ID:                    42,
		RepoID:                7,
		CommitID:              &commitID,
		GitRef:                "abc123",
		Branch:                "feature/bun",
		CIBaseBranch:          "main",
		SessionID:             "session-1",
		Agent:                 "codex",
		Model:                 "gpt-5.6-sol",
		Provider:              "openai",
		RequestedModel:        "gpt-5.6-sol",
		RequestedProvider:     "openai",
		Reasoning:             "thorough",
		JobType:               JobTypeFix,
		Status:                JobStatusRunning,
		EnqueuedAt:            time.Date(2026, time.July, 18, 15, 3, 0, 0, time.UTC),
		StartedAt:             &startedAt,
		WorkerID:              "worker-1",
		Error:                 "provider unavailable",
		Prompt:                "review this change",
		RetryCount:            2,
		DiffContent:           &diffContent,
		DirtyFiles:            []string{"a.go", "b.go"},
		Agentic:               true,
		PromptPrebuilt:        true,
		ReviewType:            "security",
		PatchID:               "patch-id",
		OutputPrefix:          "Security review",
		SkipReason:            "not applicable",
		Source:                JobSourceCI,
		ParentJobID:           &parentJobID,
		Patch:                 &patch,
		WorktreePath:          "/tmp/worktree",
		CommandLine:           "codex review abc123",
		MinSeverity:           "medium",
		BackupAgent:           "claude",
		BackupModel:           "sonnet",
		PanelRunUUID:          "panel-run",
		PanelRole:             PanelRoleMember,
		PanelName:             "default",
		PanelMemberName:       "security",
		PanelMemberIndex:      1,
		PanelMemberConfigJSON: `{"agent":"codex"}`,
		ClaimBlocked:          true,
		TokenUsage:            `{"input_tokens":10}`,
		UUID:                  "job-uuid",
		SourceMachineID:       "machine-uuid",
		UpdatedAt:             &updatedAt,
	}

	row := jobRowFromModel(want)
	var got ReviewJob
	row.applyToModel(&got)

	assert.Equal(t, want, got)
}

func TestJobColumnSetsDocumentStoreRoles(t *testing.T) {
	assert.Contains(t, sqliteJobColumns, "worker_id")
	assert.NotContains(t, postgresJobColumns, "worker_id")
	assert.Contains(t, postgresJobColumns, "source_machine_id")
	assert.Contains(t, sqliteJobColumns, "synced_at")
	assert.NotContains(t, postgresJobColumns, "synced_at")
}
