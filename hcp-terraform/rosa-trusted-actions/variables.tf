variable "notification_url" {
  type      = string
  default   = null
  sensitive = true
}

variable "ocm_client_id" {
  type      = string
  sensitive = true
}

variable "ocm_client_secret" {
  type      = string
  sensitive = true
}

variable "backplane_client_secret" {
  type      = string
  sensitive = true
}

# tflint-ignore: terraform_unused_declarations
variable "_deletion_approvals" {
  type = list(object({
    address    = string
    expires_at = string
  }))
  default = []
}

# Aurora master password. Authored in Vault — this root only passes it through
# to the regional workspace. See docs/database-and-compute.md.
variable "db_password" {
  type      = string
  sensitive = true
}
