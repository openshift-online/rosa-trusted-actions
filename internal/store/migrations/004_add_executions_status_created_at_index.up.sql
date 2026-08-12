CREATE INDEX IF NOT EXISTS idx_executions_status_created_at ON executions(status, created_at);
