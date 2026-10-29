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

variable "s3_bucket_name_suffix" {
  description = "Suffix segment of the per-region S3 bucket name (ACCOUNT-ENVIRONMENT-SUFFIX-REGION). The bucket itself is created in the regional workspace (terraform/regional/s3.tf); this value must match the regional workspace's s3_bucket_name_suffix so the IAM wildcard ARN here covers it."
  type        = string
  default     = "rosa-trusted-actions"
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
