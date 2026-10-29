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
