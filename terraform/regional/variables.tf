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

variable "ocm_client_id" {
  description = "OCM client ID (ROSA_TA_OCM_CLIENT_ID)"
  type        = string
  default     = ""
}

variable "ocm_base_url" {
  description = "OCM base URL (ROSA_TA_OCM_BASE_URL)"
  type        = string
  default     = "https://api.openshift.com"
}

variable "jwk_cert_url" {
  description = "JWK certificate URL for JWT validation (ROSA_TA_JWK_CERT_URL)"
  type        = string
  default     = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"
}

variable "backplane_url" {
  description = "Backplane API base URL (ROSA_TA_BACKPLANE_URL)"
  type        = string
}

variable "backplane_client_id" {
  description = "Backplane client ID for HMAC signing (ROSA_TA_BACKPLANE_CLIENT_ID)"
  type        = string
}

variable "allowed_accounts" {
  description = "Comma-separated AWS account IDs allowed to call the API (ROSA_TA_ALLOWED_ACCOUNTS)"
  type        = string
  default     = ""
}

variable "allowed_namespaces" {
  description = "Comma-separated Kubernetes namespaces allowed as action targets (ROSA_TA_ALLOWED_NAMESPACES)"
  type        = string
  default     = ""
}

variable "allowed_secrets" {
  description = "Comma-separated namespace/name pairs for allowed secrets (ROSA_TA_ALLOWED_SECRETS)"
  type        = string
  default     = ""
}

variable "worker_concurrency" {
  description = "Number of worker goroutines (ROSA_TA_WORKER_CONCURRENCY)"
  type        = number
  default     = 4
}

variable "worker_poll_interval" {
  description = "Worker poll interval, Go duration string (ROSA_TA_WORKER_POLL_INTERVAL)"
  type        = string
  default     = "5s"
}

variable "worker_execution_timeout" {
  description = "Max execution time per job, Go duration string (ROSA_TA_WORKER_EXECUTION_TIMEOUT)"
  type        = string
  default     = "2m"
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
