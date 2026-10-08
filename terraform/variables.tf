variable "vpc_id" {
  description = "ID of an existing VPC to deploy into. When set, vpc.tf resources are skipped and public_subnet_ids / private_subnet_ids must also be provided."
  type        = string
  default     = ""
}

variable "public_subnet_ids" {
  description = "IDs of two existing public subnets (reserved for future use / NAT gateway placement). Required when vpc_id is set."
  type        = list(string)
  default     = []
  validation {
    condition     = length(var.public_subnet_ids) == 0 || length(var.public_subnet_ids) >= 2
    error_message = "public_subnet_ids must contain at least two subnet IDs."
  }
}

variable "private_subnet_ids" {
  description = "IDs of two existing private subnets in different AZs. [0] hosts the ECS EC2 instance; both are used by the internal ALB (AWS requires subnets in at least two AZs). Required when vpc_id is set."
  type        = list(string)
  default     = []
  validation {
    condition     = length(var.private_subnet_ids) == 0 || length(var.private_subnet_ids) >= 2
    error_message = "private_subnet_ids must contain at least two subnet IDs in different AZs (internal ALB requires multi-AZ subnets)."
  }
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "app_name" {
  description = "Application name, used as prefix for all resource names"
  type        = string
  default     = "rosa-trusted-actions"
}

variable "environment" {
  description = "Deployment environment tag"
  type        = string
  default     = "prod"
}

variable "container_image" {
  description = "Full Quay.io image URI including tag — images are built by Tekton CI at quay.io/repository/redhat-user-workloads/rosa-tenant/rosa-trusted-actions."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for ECS host. t3.micro (1 GiB RAM) is sufficient for PoC with memory=512. Upgrade to t3.small (2 GiB) if workers are memory-hungry."
  type        = string
  default     = "t3.micro"
}

variable "internal_fqdn" {
  description = "Private FQDN for the API (e.g. rosa-trusted-actions.internal.company.com). Must be a subdomain of a real public domain you control — the apex zone is used only for ACM DNS-01 validation (shadow zone pattern); no A record is published publicly. Leave empty to skip HTTPS."
  type        = string
  default     = ""
}

variable "public_zone_id" {
  description = "Route 53 public hosted zone ID that owns the apex of var.internal_fqdn (e.g. the zone for internal.company.com). Used exclusively to place the ACM DNS-01 validation CNAME — no A record is created here, so the service remains unreachable from the public internet. When empty, ACM validation records must be added manually and var.alb_certificate_arn must be set directly."
  type        = string
  default     = ""
}

variable "manage_validation_record" {
  description = "When true, this deployment owns the ACM DNS-01 validation CNAME in the public Route 53 zone. Exactly one regional deployment must set this to true — the one that applies first and whose lifecycle governs the shared record. All other regional deployments set this to false and wait for ACM to validate using the record created by the owning deployment. Setting this to true in more than one deployment simultaneously will cause a state conflict on the shared Route 53 record."
  type        = bool
  default     = false
}

variable "alb_certificate_arn" {
  description = "ACM certificate ARN for the HTTPS listener. Populated automatically when var.internal_fqdn + var.public_zone_id are set (see acm.tf). Override manually when DNS is not in Route 53."
  type        = string
  default     = ""
}

variable "s3_bucket_name" {
  description = "S3 bucket for execution outputs and logs (ROSA_TA_S3_BUCKET)"
  type        = string
}

variable "enable_worm" {
  description = "Enable WORM object lock retention (COMPLIANCE mode). Set true for production. The bucket is always created with object_lock_enabled = true (cannot be changed after creation), but without a retention rule objects are freely deletable."
  type        = bool
  default     = false
}

variable "retention_days" {
  description = "WORM object lock retention period in days (COMPLIANCE mode — objects cannot be deleted before this period, even by the bucket owner)"
  type        = number
  default     = 1
  validation {
    condition     = var.retention_days > 0 && floor(var.retention_days) == var.retention_days
    error_message = "retention_days must be a positive integer."
  }
}

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
  description = "Max time per execution, Go duration string (ROSA_TA_WORKER_EXECUTION_TIMEOUT)"
  type        = string
  default     = "2m"
}

# Sensitive — store in tfvars or environment, never commit
variable "ocm_client_secret" {
  # TODO: ocm_client_secret and backplane_client_secret are separate Terraform
  # variables and Secrets Manager keys today. In practice both are the same
  # service-account credential. Merge into a single variable and secret key
  # when the app-level env vars are unified.
  description = "OCM client secret (ROSA_TA_OCM_CLIENT_SECRET)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "ocm_token" {
  description = "OCM offline token, alternative to client_secret (ROSA_TA_OCM_TOKEN)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "backplane_client_secret" {
  # TODO: see ocm_client_secret above — merge these once the app unifies its credential env vars.
  description = "Backplane HMAC signing secret (ROSA_TA_BACKPLANE_CLIENT_SECRET)"
  type        = string
  sensitive   = true
}

# Phase 2 only (unused in Phase 1)
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
