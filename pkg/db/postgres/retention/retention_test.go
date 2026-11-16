package retention_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	dbpostgres "github.com/inngest/inngest/pkg/db/postgres"
	"github.com/inngest/inngest/pkg/db/postgres/retention"
	"github.com/inngest/inngest/tests/testutil"
	"github.com/stretchr/testify/require"
)

const acct = "00000000-0000-0000-0000-000000000001"

// runID returns a 26-character ID, matching trace_runs.run_id CHAR(26).
func runID(n int) string { return fmt.Sprintf("%026d", n) }

func ms(t time.Time) int64 { return t.UnixMilli() }

func TestRunRejectsInvalidConfig(t *testing.T) {
	_, err := retention.Run(t.Context(), nil, retention.Config{Retention: 0, BatchSize: 1})
	require.Error(t, err)

	_, err = retention.Run(t.Context(), nil, retention.Config{Retention: time.Hour, BatchSize: 0})
	require.Error(t, err)
}

func TestRun(t *testing.T) {
	if os.Getenv("TEST_DATABASE") != "postgres" {
		t.Skip("set TEST_DATABASE=postgres to run against a real Postgres")
	}

	ctx := t.Context()
	pc, err := testutil.StartPostgres(t)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Terminate(context.Background()) })

	// Open runs the migrations, so this also exercises migration 000011.
	conn, err := dbpostgres.Open(ctx, dbpostgres.Options{URI: pc.URI, ForTest: true})
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	seed(t, conn)

	// BatchSize 1 forces every table through the batch loop.
	cfg := retention.Config{Retention: 30 * 24 * time.Hour, BatchSize: 1, DryRun: true}

	wantCounts := map[string]int64{
		"spans":             1,
		"traces":            1,
		"event_batches":     0,
		"trace_runs":        1,
		"history":           1,
		"function_runs":     1,
		"function_finishes": 1,
		"events":            1,
	}

	dry, err := retention.Run(ctx, conn, cfg)
	require.NoError(t, err)
	require.Equal(t, wantCounts, counts(dry))
	require.Equal(t, int64(3), countRows(t, conn, "trace_runs"), "dry run must not delete")

	cfg.DryRun = false
	deleted, err := retention.Run(ctx, conn, cfg)
	require.NoError(t, err)
	require.Equal(t, wantCounts, counts(deleted))

	// Run 1 is the only expired finished run. Run 2 never ended and run 3 is
	// inside the window, so both must survive.
	require.Equal(t, int64(2), countRows(t, conn, "trace_runs"))
	require.Equal(t, int64(1), countRows(t, conn, "function_finishes"))
	require.Equal(t, int64(2), countRows(t, conn, "events"))
}

// seed inserts three runs:
//   - run 1: finished 60 days ago, so it is expired and deleted
//   - run 2: still running, started 60 days ago, so it is kept
//   - run 3: finished just now, so it is kept
//
// Each run has an event; event 1 is only referenced by run 1, event 2 by the
// unfinished run 2, and event 3 is recent.
func seed(t *testing.T, db *sql.DB) {
	t.Helper()

	now := time.Now().UTC()
	old := now.Add(-60 * 24 * time.Hour)

	runs := []struct {
		id      string
		status  int
		queued  time.Time
		ended   int64
		finAt   *time.Time
		eventID int
	}{
		{id: runID(1), status: 300, queued: old, ended: ms(old), finAt: &old, eventID: 1},
		{id: runID(2), status: 200, queued: old, ended: 0, finAt: nil, eventID: 2},
		{id: runID(3), status: 300, queued: now, ended: ms(now), finAt: &now, eventID: 3},
	}

	for _, r := range runs {
		evID := []byte(fmt.Sprintf("event-%d", r.eventID))
		runBytes := []byte(r.id)

		exec(t, db, `INSERT INTO trace_runs (run_id, account_id, workspace_id, app_id, function_id,
			trace_id, queued_at, started_at, ended_at, status, source_id, trigger_ids, is_debounce)
			VALUES ($1, $2, $2, $2, $2, $3, $4, $4, $5, $6, '', $3, false)`,
			r.id, acct, []byte{}, ms(r.queued), r.ended, r.status)

		exec(t, db, `INSERT INTO spans (span_id, trace_id, name, start_time, end_time,
			account_id, app_id, function_id, run_id, env_id)
			VALUES ($1, $1, 'step.run', $2, $2, $3, $3, $3, $1, $3)`,
			r.id, now, acct)

		exec(t, db, `INSERT INTO traces (timestamp, timestamp_unix_ms, trace_id, span_id, span_name,
			span_kind, service_name, resource_attributes, scope_name, scope_version, span_attributes,
			duration, status_code, events, links, run_id)
			VALUES ($1, $2, $3, $3, 'step.run', 'internal', 'inngest', $4, '', '', $4, 0, 'OK', $4, $4, $5)`,
			now, ms(now), r.id, []byte{}, r.id)

		exec(t, db, `INSERT INTO events (internal_id, event_id, event_name, event_ts, received_at)
			VALUES ($1, $2, 'test/event', $3, $3)`,
			evID, fmt.Sprintf("evt-%d", r.eventID), r.queued)

		exec(t, db, `INSERT INTO function_runs (run_id, function_id, function_version, event_id, run_started_at)
			VALUES ($1, $2, 1, $3, $4)`,
			runBytes, acct, evID, r.queued)

		exec(t, db, `INSERT INTO history (run_id, event_id, function_version, idempotency_key, type, attempt, created_at)
			VALUES ($1, $2, 1, 'k', 'FunctionStarted', 0, $3)`,
			runBytes, evID, r.queued)

		if r.finAt != nil {
			exec(t, db, `INSERT INTO function_finishes (run_id, status, created_at)
				VALUES ($1, 'Completed', $2)`,
				runBytes, *r.finAt)
		}
	}
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), q, args...)
	require.NoError(t, err)
}

func countRows(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var n int64
	err := db.QueryRowContext(t.Context(), fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&n)
	require.NoError(t, err)
	return n
}

func counts(results []retention.Result) map[string]int64 {
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.Table] = r.Rows
	}
	return m
}
