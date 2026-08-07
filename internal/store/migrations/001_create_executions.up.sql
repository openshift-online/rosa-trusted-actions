CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    action             TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending',
    approval_state     TEXT,
    username           TEXT,
    target_cluster     TEXT NOT NULL,
    jira               TEXT,
    dry_run            INTEGER,
    force              INTEGER,
    params             TEXT,
    scope              TEXT,
    type               TEXT,
    revision           TEXT,
    manifest_work_name TEXT,
    output_path        TEXT,
    output_status      TEXT,
    runner_seconds     INTEGER,
    upload_seconds     INTEGER,
    duration_seconds   INTEGER,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    completed_at       TEXT
);

CREATE INDEX IF NOT EXISTS idx_executions_status ON executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_action ON executions(action);
CREATE INDEX IF NOT EXISTS idx_executions_target_cluster ON executions(target_cluster);
CREATE INDEX IF NOT EXISTS idx_executions_username ON executions(username);
CREATE INDEX IF NOT EXISTS idx_executions_created_at ON executions(created_at);
