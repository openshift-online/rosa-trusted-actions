variable "notification_url" {
  type      = string
  default   = null
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
