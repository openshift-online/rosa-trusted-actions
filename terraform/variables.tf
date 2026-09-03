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

variable "domain_name" {
  description = "Public domain name for the API (e.g. trusted-actions.example.com). Required for ACM auto-provisioning. Leave empty to skip HTTPS."
  type        = string
  default     = ""
}

variable "route53_zone_id" {
  description = "Route53 hosted zone ID for var.domain_name. When set, ACM DNS validation records are created automatically. When empty (domain not in Route53), add the CNAME records manually and set var.alb_certificate_arn directly."
  type        = string
  default     = ""
}

variable "alb_certificate_arn" {
  description = "ACM certificate ARN for HTTPS listener. Populated automatically when var.domain_name + var.route53_zone_id are set (see acm.tf). Override manually if DNS is not in Route53."
  type        = string
  default     = ""
}

variable "s3_bucket_name" {
  description = "S3 bucket for execution outputs and logs (ROSA_TA_S3_BUCKET)"
  type        = string
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
