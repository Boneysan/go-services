-- Task 4.2c tail: persistent streak ledger for the dual-write diff job
-- (tools/save_diff/compare_saves.py). Each automated run inserts one row.
-- A consecutive run of mismatch_count = 0 rows is the "streak" the Step 3
-- backup_service retirement gate (30 real calendar days, see PROGRESS.md
-- Task 4.2c) is measured against. This table is observability only — it
-- does not flip any cutover flag itself.
CREATE TABLE IF NOT EXISTS dual_write_diff_log (
    id SERIAL PRIMARY KEY,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    total_compared INTEGER NOT NULL,
    mismatch_count INTEGER NOT NULL,
    mismatches JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX IF NOT EXISTS dual_write_diff_log_checked_at_idx ON dual_write_diff_log (checked_at DESC);
