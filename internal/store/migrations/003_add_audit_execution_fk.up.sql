CREATE TABLE IF NOT EXISTS audit_entries_new (
    id              TEXT PRIMARY KEY,
    timestamp       TEXT NOT NULL,
    method          TEXT NOT NULL,
    path            TEXT NOT NULL,
    username        TEXT NOT NULL,
    status_code     INTEGER NOT NULL,
    action          TEXT,
    execution_id    TEXT REFERENCES executions(id),
    jira            TEXT,
    approval_state  TEXT,
    target_cluster  TEXT
);

INSERT INTO audit_entries_new SELECT * FROM audit_entries;

DROP TABLE audit_entries;

ALTER TABLE audit_entries_new RENAME TO audit_entries;

CREATE INDEX IF NOT EXISTS idx_audit_entries_timestamp ON audit_entries(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_entries_action ON audit_entries(action);
CREATE INDEX IF NOT EXISTS idx_audit_entries_username ON audit_entries(username);
CREATE INDEX IF NOT EXISTS idx_audit_entries_target_cluster ON audit_entries(target_cluster);
CREATE INDEX IF NOT EXISTS idx_audit_entries_method ON audit_entries(method);
