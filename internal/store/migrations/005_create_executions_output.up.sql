ALTER TABLE executions DROP COLUMN output_path;
ALTER TABLE executions DROP COLUMN output_status;
CREATE TABLE executions_output (
    id                 TEXT PRIMARY KEY,
    exec_id            TEXT NOT NULL,
    message            TEXT NOT NULL,
    resources          TEXT NOT NULL,
    FOREIGN KEY (exec_id) REFERENCES executions(id)
);
CREATE INDEX idx_executions_output_exec_id ON executions_output(exec_id);