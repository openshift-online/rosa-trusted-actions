package store

// Column lists used by both SQLiteStore and PostgresStore. The order must
// match the corresponding *Row struct field order for sqlx StructScan.
const executionColumns = `id, action, status, approval_state, username, target_cluster,
	jira, dry_run, force, params, scope, type, revision,
	manifest_work_name,
	runner_seconds, upload_seconds, duration_seconds,
	created_at, updated_at, completed_at`

const outputColumns = `id, exec_id, message, resources`

const auditColumns = `id, timestamp, method, path, username, status_code,
	action, execution_id, jira, approval_state, target_cluster`

// buildExecutionWhere constructs WHERE clauses for execution queries. Placeholders
// are written as "?" and must be rebound for Postgres via db.Rebind before use.
//
// The Since filter is intentionally excluded: timestamp formatting differs
// between SQLite (fixed-width TEXT) and Postgres (native TIMESTAMPTZ). Each
// store appends the Since clause itself after calling this function.
func buildExecutionWhere(filter ExecutionFilter) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}

	if filter.Status != nil {
		clauses = append(clauses, "status = ?")
		args = append(args, *filter.Status)
	}
	if filter.Action != nil {
		clauses = append(clauses, "action = ?")
		args = append(args, *filter.Action)
	}
	if filter.Target != nil {
		clauses = append(clauses, "target_cluster = ?")
		args = append(args, *filter.Target)
	}
	if filter.Operator != nil {
		clauses = append(clauses, "username = ?")
		args = append(args, *filter.Operator)
	}
	if filter.Scope != nil {
		clauses = append(clauses, "scope = ?")
		args = append(args, *filter.Scope)
	}
	if filter.Type != nil {
		clauses = append(clauses, "type = ?")
		args = append(args, *filter.Type)
	}
	if filter.ApprovalState != nil {
		clauses = append(clauses, "approval_state = ?")
		args = append(args, *filter.ApprovalState)
	}
	if filter.DryRun != nil {
		clauses = append(clauses, "dry_run = ?")
		args = append(args, *filter.DryRun)
	}
	if filter.Force != nil {
		clauses = append(clauses, "force = ?")
		args = append(args, *filter.Force)
	}

	return clauses, args
}

// buildAuditWhere constructs WHERE clauses for audit queries. Same placeholder
// convention as buildExecutionWhere; Since is excluded for the same reason.
func buildAuditWhere(filter AuditFilter) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}

	if filter.Action != nil {
		clauses = append(clauses, "action = ?")
		args = append(args, *filter.Action)
	}
	if filter.Target != nil {
		clauses = append(clauses, "target_cluster = ?")
		args = append(args, *filter.Target)
	}
	if filter.Operator != nil {
		clauses = append(clauses, "username = ?")
		args = append(args, *filter.Operator)
	}
	if filter.Method != nil {
		clauses = append(clauses, "method = ?")
		args = append(args, *filter.Method)
	}
	if filter.ApprovalState != nil {
		clauses = append(clauses, "approval_state = ?")
		args = append(args, *filter.ApprovalState)
	}

	return clauses, args
}
