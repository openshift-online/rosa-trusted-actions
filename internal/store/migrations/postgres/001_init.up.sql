CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    action             TEXT        NOT NULL,
    status             TEXT        NOT NULL DEFAULT 'pending',
    approval_state     TEXT,
    username           TEXT,
    target_cluster     TEXT        NOT NULL,
    jira               TEXT,
    dry_run            BOOLEAN,
    force              BOOLEAN,
    params             TEXT,
    scope              TEXT,
    type               TEXT,
    revision           TEXT,
    manifest_work_name TEXT,
    runner_seconds     INTEGER,
    upload_seconds     INTEGER,
    duration_seconds   INTEGER,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    completed_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_executions_status            ON executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_action            ON executions(action);
CREATE INDEX IF NOT EXISTS idx_executions_target_cluster    ON executions(target_cluster);
CREATE INDEX IF NOT EXISTS idx_executions_username          ON executions(username);
CREATE INDEX IF NOT EXISTS idx_executions_created_at        ON executions(created_at);
CREATE INDEX IF NOT EXISTS idx_executions_status_created_at ON executions(status, created_at);

CREATE TABLE IF NOT EXISTS executions_output (
    id        TEXT PRIMARY KEY,
    exec_id   TEXT NOT NULL REFERENCES executions(id),
    message   TEXT NOT NULL,
    resources TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_executions_output_exec_id ON executions_output(exec_id);

CREATE TABLE IF NOT EXISTS audit_entries (
    id              TEXT        PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL,
    method          TEXT        NOT NULL,
    path            TEXT        NOT NULL,
    username        TEXT        NOT NULL,
    status_code     INTEGER     NOT NULL,
    action          TEXT,
    execution_id    TEXT        REFERENCES executions(id),
    jira            TEXT,
    approval_state  TEXT,
    target_cluster  TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_entries_timestamp      ON audit_entries(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_entries_action         ON audit_entries(action);
CREATE INDEX IF NOT EXISTS idx_audit_entries_username       ON audit_entries(username);
CREATE INDEX IF NOT EXISTS idx_audit_entries_target_cluster ON audit_entries(target_cluster);
CREATE INDEX IF NOT EXISTS idx_audit_entries_method         ON audit_entries(method);
