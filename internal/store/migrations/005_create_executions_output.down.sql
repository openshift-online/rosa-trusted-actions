DROP INDEX idx_executions_output_exec_id;
DROP TABLE executions_output;
ALTER TABLE executions ADD COLUMN output_path TEXT;
ALTER TABLE executions ADD COLUMN output_status TEXT;