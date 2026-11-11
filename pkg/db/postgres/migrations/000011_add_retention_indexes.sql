-- +goose NO TRANSACTION
-- +goose Up

-- Gating predicates for the retention cleanup (see docs/POSTGRES_RETENTION.md).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_trace_runs_status_ended_at
  ON trace_runs (status, ended_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_function_finishes_created_at
  ON function_finishes (created_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_traces_run_id
  ON traces (run_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_traces_run_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_function_finishes_created_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_trace_runs_status_ended_at;
