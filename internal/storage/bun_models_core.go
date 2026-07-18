package storage

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// dbTime is the shared nullable timestamp representation for Bun rows.
// PostgreSQL returns native time.Time values while SQLite returns text.
type dbTime struct {
	Time  time.Time
	Valid bool
}

func (t *dbTime) Scan(value any) error {
	switch value := value.(type) {
	case nil:
		*t = dbTime{}
		return nil
	case time.Time:
		t.Time = value
		t.Valid = true
		return nil
	case string:
		return t.scanString(value)
	case []byte:
		return t.scanString(string(value))
	default:
		return fmt.Errorf("scan database time from %T", value)
	}
}

func (t *dbTime) scanString(value string) error {
	if value == "" {
		*t = dbTime{}
		return nil
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			t.Time = parsed
			t.Valid = true
			return nil
		}
	}
	return fmt.Errorf("scan database time %q", value)
}

func (t dbTime) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.Time.Format(time.RFC3339Nano), nil
}

func dbTimeFromValue(value time.Time) dbTime {
	return dbTime{Time: value, Valid: true}
}

func dbTimeFromPointer(value *time.Time) dbTime {
	if value == nil {
		return dbTime{}
	}
	return dbTime{Time: *value, Valid: true}
}

func (t dbTime) pointer() *time.Time {
	if !t.Valid {
		return nil
	}
	value := t.Time
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

type repoRow struct {
	bun.BaseModel `bun:"table:repos,alias:r"`
	ID            int64   `bun:"id,pk,autoincrement"`
	RootPath      string  `bun:"root_path"`
	Name          string  `bun:"name"`
	Identity      *string `bun:"identity"`
	CreatedAt     dbTime  `bun:"created_at"`
}

func (row repoRow) toModel() Repo {
	return Repo{
		ID:        row.ID,
		RootPath:  row.RootPath,
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
		Identity:  stringValue(row.Identity),
	}
}

type commitRow struct {
	bun.BaseModel `bun:"table:commits,alias:c"`
	ID            int64  `bun:"id,pk,autoincrement"`
	RepoID        int64  `bun:"repo_id"`
	SHA           string `bun:"sha"`
	Author        string `bun:"author"`
	Subject       string `bun:"subject"`
	Timestamp     dbTime `bun:"timestamp"`
	CreatedAt     dbTime `bun:"created_at"`
}

func (row commitRow) toModel() Commit {
	return Commit{
		ID:        row.ID,
		RepoID:    row.RepoID,
		SHA:       row.SHA,
		Author:    row.Author,
		Subject:   row.Subject,
		Timestamp: row.Timestamp.Time,
		CreatedAt: row.CreatedAt.Time,
	}
}
