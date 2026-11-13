// Package retention deletes finished run history from the self-hosted Postgres
// store. The gating rules, and why age alone is not enough, are in
// docs/POSTGRES_RETENTION.md.
package retention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// terminalStatuses are the trace_runs.status codes of runs that have ended.
// See pkg/enums/run_status.go.
const terminalStatuses = "50, 300, 400, 500, 600"

// expiredTraceRuns selects ended trace runs older than $1. It gates the trace
// tables. ended_at > 0 keeps a terminal run whose end time was never written
// (zero or negative), since it has no age to compare against.
const expiredTraceRuns = `SELECT run_id FROM trace_runs
	WHERE status IN (` + terminalStatuses + `)
	AND ended_at > 0
	AND ended_at < (extract(epoch FROM now() - $1::interval) * 1000)::bigint`

// expiredFunctionRuns selects runs that finished more than $1 ago. A row in
// function_finishes exists only once a run has finished, so this never selects
// a run that is still executing. It gates function_runs and history.
const expiredFunctionRuns = `SELECT run_id FROM function_finishes
	WHERE created_at < now() - $1::interval`

// Config controls a cleanup pass.
type Config struct {
	// Retention is how long finished history is kept. Runs that have not ended
	// are never deleted, whatever their age.
	Retention time.Duration
	// BatchSize caps the rows each DELETE statement removes.
	BatchSize int
	// DryRun counts the rows that would be deleted without deleting them.
	DryRun bool
}

// Result is the number of rows matched in one table. In a dry run it is the
// number that would be deleted.
type Result struct {
	Table string
	Rows  int64
}

// step is one table's delete predicate. Steps run in order: children before
// the anchor they are gated on, so each anchor's children stay selectable
// until the anchor itself is deleted last.
type step struct {
	table string
	where string
}

var steps = []step{
	{table: "spans", where: `run_id IN (` + expiredTraceRuns + `)`},
	{table: "traces", where: `run_id IN (` + expiredTraceRuns + `)`},
	{table: "event_batches", where: `run_id IN (` + expiredTraceRuns + `)`},
	{table: "trace_runs", where: `status IN (` + terminalStatuses + `) AND ended_at > 0 AND ended_at < (extract(epoch FROM now() - $1::interval) * 1000)::bigint`},
	{table: "history", where: `run_id IN (` + expiredFunctionRuns + `)`},
	{table: "function_runs", where: `run_id IN (` + expiredFunctionRuns + `)`},
	{table: "function_finishes", where: `created_at < now() - $1::interval`},
	// Events are not 1:1 with runs, so they have their own age and are kept
	// while any unfinished run still references them.
	{table: "events", where: `received_at < now() - $1::interval AND NOT EXISTS (
		SELECT 1 FROM function_runs fr
		LEFT JOIN function_finishes ff ON ff.run_id = fr.run_id
		WHERE fr.event_id = events.internal_id AND ff.run_id IS NULL
	)`},
}

// Run applies one cleanup pass. Each table is deleted in batches until a batch
// removes nothing. In dry-run mode it only counts.
func Run(ctx context.Context, db *sql.DB, cfg Config) ([]Result, error) {
	if cfg.Retention <= 0 {
		return nil, errors.New("retention must be positive")
	}
	if cfg.BatchSize <= 0 {
		return nil, errors.New("batch size must be positive")
	}

	interval := fmt.Sprintf("%d seconds", int64(cfg.Retention.Seconds()))

	results := make([]Result, 0, len(steps))
	for _, s := range steps {
		if cfg.DryRun {
			var n int64
			// countSQL only joins package constants. The interval is bound as $1, never interpolated.
			// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
			if err := db.QueryRowContext(ctx, countSQL(s), interval).Scan(&n); err != nil {
				return results, fmt.Errorf("counting %s: %w", s.table, err)
			}
			results = append(results, Result{Table: s.table, Rows: n})
			continue
		}

		n, err := deleteBatches(ctx, db, s, interval, cfg.BatchSize)
		results = append(results, Result{Table: s.table, Rows: n})
		if err != nil {
			return results, fmt.Errorf("deleting from %s: %w", s.table, err)
		}
	}
	return results, nil
}

func countSQL(s step) string {
	return fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s`, s.table, s.where)
}

// deleteSQL uses a ctid subquery because a plain DELETE has no LIMIT. $1 is the
// retention interval and $2 the batch size.
func deleteSQL(s step) string {
	return fmt.Sprintf(
		`DELETE FROM %[1]s WHERE ctid IN (SELECT ctid FROM %[1]s WHERE %[2]s LIMIT $2)`,
		s.table, s.where,
	)
}

func deleteBatches(ctx context.Context, db *sql.DB, s step, interval string, batch int) (int64, error) {
	q := deleteSQL(s)

	var total int64
	for {
		res, err := db.ExecContext(ctx, q, interval, batch)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n == 0 {
			return total, nil
		}
	}
}
