variable "aws_region" {
  description = "AWS region for the provider. Does not affect resource scope — IAM, S3, and Secrets Manager are globally accessible. Use the account's primary region."
  type        = string
  default     = "us-east-1"
}

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

variable "s3_bucket_name" {
  description = "Globally unique S3 bucket name for execution outputs and audit logs."
  type        = string
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

variable "internal_fqdn" {
  description = "Private FQDN for the API (e.g. rosa-trusted-actions.internal.company.com). Used here only to create the ACM DNS-01 validation CNAME in the public zone. Leave empty to skip certificate management in global."
  type        = string
  default     = ""
}

variable "public_zone_id" {
  description = "Route 53 public hosted zone ID for the apex of internal_fqdn. The ACM DNS-01 validation CNAME is the only record written here — no A record is ever published."
  type        = string
  default     = ""
}

# Sensitive — store in HCP Terraform workspace as sensitive variables, never in source
variable "ocm_client_secret" {
  description = "OCM client secret (ROSA_TA_OCM_CLIENT_SECRET)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "ocm_token" {
  description = "OCM offline token, alternative to ocm_client_secret (ROSA_TA_OCM_TOKEN)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "backplane_client_secret" {
  description = "Backplane HMAC signing secret (ROSA_TA_BACKPLANE_CLIENT_SECRET)"
  type        = string
  sensitive   = true
}
