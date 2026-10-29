_deletion_approvals = [
  {
    address    = "tfe_run_trigger.stage_depends_on_global"
    expires_at = "2026-10-31T00:00:00Z" # e.g. "2026-08-01T00:00:00Z"
  },
  {
    address    = "tfe_workspace_settings.global_state_sharing"
    expires_at = "2026-10-31T00:00:00Z" # e.g. "2026-08-01T00:00:00Z"
  },
  {
    address    = "module.rosa_trusted_actions.tfe_variable.this[\"rosa-trusted-actions-global:terraform:s3_bucket_name\"]"
    expires_at = "2026-10-31T00:00:00Z" # e.g. "2026-08-01T00:00:00Z"
  },
]
