# ── Networking ────────────────────────────────────────────────────────────────

variable "aws_region" {
  description = "AWS region for this deployment."
  type        = string
  default     = "us-east-1"
}

variable "vpc_id" {
  description = "ID of an existing VPC. When set, vpc.tf resources are skipped and private_subnet_ids must also be provided."
  type        = string
  default     = ""
}

variable "public_subnet_ids" {
  description = "IDs of two existing public subnets (used for NAT gateway when create_vpc = true). Required when vpc_id is set and a managed VPC is not created."
  type        = list(string)
  default     = []
  validation {
    condition     = length(var.public_subnet_ids) == 0 || length(var.public_subnet_ids) >= 2
    error_message = "public_subnet_ids must contain at least two subnet IDs."
  }
}

variable "private_subnet_ids" {
  description = "IDs of two existing private subnets in different AZs. [0] hosts the ECS EC2 instance; both are used by the internal ALB."
  type        = list(string)
  default     = []
  validation {
    condition     = length(var.private_subnet_ids) == 0 || length(var.private_subnet_ids) >= 2
    error_message = "private_subnet_ids must contain at least two subnet IDs in different AZs."
  }
}

# ── Identity ──────────────────────────────────────────────────────────────────

variable "app_name" {
  description = "Application name, used as a prefix for all resource names."
  type        = string
  default     = "rosa-trusted-actions"
}

variable "environment" {
  description = "Deployment environment tag (e.g. stage, prod)."
  type        = string
  default     = "prod"
}

# ── Storage ───────────────────────────────────────────────────────────────────

variable "s3_bucket_name_suffix" {
  description = "Suffix segment of the S3 bucket name (ACCOUNT-ENVIRONMENT-SUFFIX-REGION). Must match the global workspace's s3_bucket_name_suffix so its IAM wildcard ARN covers this bucket."
  type        = string
  default     = "rosa-trusted-actions"
}

variable "enable_worm" {
  description = "Enable WORM object lock retention (COMPLIANCE mode). The bucket is always created with object_lock_enabled = true, but without a retention rule objects are freely deletable. Set true for production."
  type        = bool
  default     = false
}

variable "retention_days" {
  description = "WORM object lock retention period in days (COMPLIANCE mode)."
  type        = number
  default     = 1
  validation {
    condition     = var.retention_days > 0 && floor(var.retention_days) == var.retention_days
    error_message = "retention_days must be a positive integer."
  }
}

# ── Compute ───────────────────────────────────────────────────────────────────

variable "container_image" {
  description = "Full image URI including digest, e.g. quay.io/redhat-user-workloads/...@sha256:..."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for the ECS host."
  type        = string
  default     = "t3.micro"
}

# ── DNS & TLS ─────────────────────────────────────────────────────────────────

variable "internal_fqdn" {
  description = "Private FQDN for the API (e.g. rosa-trusted-actions.internal.company.com). Creates the Route 53 Private Hosted Zone, alias A record, and (with public_zone_id) the regional ACM certificate. Leave empty to skip DNS and HTTPS."
  type        = string
  default     = ""
}

variable "public_zone_id" {
  description = "Route 53 public hosted zone ID for the apex of internal_fqdn. Required for ACM certificate issuance — the validation CNAME is managed by the global workspace. Leave empty to skip the ACM certificate (HTTP only)."
  type        = string
  default     = ""
}

variable "alb_certificate_arn" {
  description = "ACM certificate ARN for the HTTPS listener. Set instead of public_zone_id when managing the certificate outside Terraform."
  type        = string
  default     = ""
}

# ── Application config ────────────────────────────────────────────────────────

variable "app_config" {
  description = "Complete application configuration file (YAML), uploaded to S3 as config.yaml. Covers everything the config-file schema supports: workers, actions allowlists (namespaces, secrets, AWS regions/operations), aws/backplane proxy settings, and roles. Settings here must NOT also be set as environment variables in the task definition — env overrides the file silently. Credentials never belong here: they stay in Secrets Manager."
  type        = string

  # Structure check only — the full schema is validated by the Go loader at
  # container startup (and in CI before apply). yamldecode fails the
  # validation when the YAML is malformed. roles may come from the file or
  # from the deprecated var.roles (merged in ecs.tf) — either satisfies the
  # requirement, because roles is required in the deployed config when auth
  # is enabled.
  validation {
    condition = alltrue([
      try(length(yamldecode(var.app_config)) > 0, false),
      can(yamldecode(var.app_config).roles) || var.roles != null,
    ])
    error_message = "app_config must be valid YAML with a non-empty top-level map, and roles must come from the file or the deprecated roles variable."
  }

  # Default reproduces the values previously spread across the roles,
  # worker_* and allowed_* variables, so an unconfigured deployment behaves
  # as before. aws_allowed_regions is present because the server refuses to
  # start without it (no built-in default) and matches the variable default
  # for aws_region. allowed_namespaces is deliberately explicit: an empty
  # list puts the authorizer in permissive mode, so the deployed config must
  # state what it wants rather than inherit a silent "allow all".
  default = <<-YAML
    actions:
      aws_allowed_regions: [us-east-1]
      allowed_namespaces: []
      allowed_secrets: []
    roles:
      - id: SREP
        amsResource: BackplaneOsdSrepResource
      - id: ConfigurationAnomalyDetection
        amsResource: BackplaneOsdCadResource
      - id: ROSAAiAgent
        amsResource: BackplaneOsdAiAgentResource
    workers:
      concurrency: 4
      poll_interval: 5s
      execution_timeout: 2m
  YAML
}

# ── Deprecated per-setting variables (remove after one release) ───────────────
#
# app_config replaced these. They are re-added for one commit so existing
# tfvars/HCP workspace configurations keep working: when one of these is set,
# its value is applied as an environment variable on the task definition —
# env overrides the config file in the Go loader, so the old variable wins,
# which is the behaviour callers had before the migration. A non-empty old
# variable together with the same key in app_config produces a validation
# error rather than a silent override.
#
# New deployments should set app_config only. These variables and their env
# entries in ecs.tf are removed in the next change.

variable "worker_concurrency" {
  description = "DEPRECATED: use app_config (workers.concurrency). Still applied as ROSA_TA_WORKER_CONCURRENCY when set, overriding the file."
  type        = number
  default     = null
}

variable "worker_poll_interval" {
  description = "DEPRECATED: use app_config (workers.poll_interval). Still applied as ROSA_TA_WORKER_POLL_INTERVAL when set, overriding the file."
  type        = string
  default     = null
}

variable "worker_execution_timeout" {
  description = "DEPRECATED: use app_config (workers.execution_timeout). Still applied as ROSA_TA_WORKER_EXECUTION_TIMEOUT when set, overriding the file."
  type        = string
  default     = null
}

variable "roles" {
  description = "DEPRECATED: use app_config (roles). Ignored unless app_config's roles block is absent — the file value wins when both are present."
  type = list(object({
    id           = string
    ams_resource = string
  }))
  default = null
}

variable "allowed_namespaces" {
  description = "DEPRECATED: use app_config (actions.allowed_namespaces). Still applied as ROSA_TA_ALLOWED_NAMESPACES when set, overriding the file."
  type        = string
  default     = null
}

variable "allowed_secrets" {
  description = "DEPRECATED: use app_config (actions.allowed_secrets). Still applied as ROSA_TA_ALLOWED_SECRETS when set, overriding the file."
  type        = string
  default     = null
}

variable "ocm_client_id" {
  description = "DEPRECATED: use app_config (ocm.client_id). Still applied as ROSA_TA_OCM_CLIENT_ID when set, overriding the file."
  type        = string
  default     = null
}

variable "backplane_url" {
  description = "DEPRECATED: use app_config (backplane.base_url). Still applied as ROSA_TA_BACKPLANE_URL when set, overriding the file."
  type        = string
  default     = null
}

variable "backplane_client_id" {
  description = "DEPRECATED: use app_config (backplane.client_id). Still applied as ROSA_TA_BACKPLANE_CLIENT_ID when set, overriding the file."
  type        = string
  default     = null
}


variable "ocm_base_url" {
  description = "DEPRECATED: use app_config (ocm.base_url). Applied as ROSA_TA_OCM_BASE_URL when set, overriding the file. Unset (null) falls back to the file and then the server's built-in default."
  type        = string
  default     = null
}

variable "jwk_cert_url" {
  description = "JWK certificate URL for JWT validation (ROSA_TA_JWK_CERT_URL)"
  type        = string
  default     = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"
}

variable "allowed_accounts" {
  description = "Comma-separated AWS account IDs allowed to call the API (ROSA_TA_ALLOWED_ACCOUNTS)"
  type        = string
  default     = ""
}



# ── Phase 2 placeholders ──────────────────────────────────────────────────────

variable "db_username" {
  description = "Aurora master username (Phase 2)"
  type        = string
  default     = "trusted_actions"
}

variable "db_name" {
  description = "Aurora database name (Phase 2)"
  type        = string
  default     = "trusted_actions"
}
