resource "aws_secretsmanager_secret" "app" {
  name                    = "${var.app_name}/prod"
  description             = "Sensitive env vars for ${var.app_name}"
  recovery_window_in_days = 0 # immediate deletion; set to 30 for production
}

resource "aws_secretsmanager_secret_version" "app" {
  secret_id = aws_secretsmanager_secret.app.id
  secret_string = jsonencode({
    # TODO: merge ocm_client_secret and backplane_client_secret into one key once
    # ROSA_TA_OCM_CLIENT_SECRET and ROSA_TA_BACKPLANE_CLIENT_SECRET are unified.
    ocm_client_secret       = var.ocm_client_secret
    ocm_token               = var.ocm_token
    backplane_client_secret = var.backplane_client_secret
  })

  lifecycle {
    ignore_changes = [secret_string] # allow out-of-band rotation without Terraform drift
  }
}
