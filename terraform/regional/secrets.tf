# Aurora master credentials for the ECS task.
#
# ECS resolves `secrets` only from Secrets Manager or SSM Parameter Store, so
# the password has to live in one of them even though Terraform already holds
# it. This secret is regional rather than global so the value never enters the
# global workspace; global grants access by predictable name instead — see
# aws_iam_policy.read_secrets in terraform/global/iam.tf.

resource "aws_secretsmanager_secret" "db" {
  name                    = "${var.app_name}/db"
  description             = "Aurora master credentials for ${var.app_name}"
  recovery_window_in_days = 0 # immediate deletion; set to 30 for production
}

# The {username, password} shape matches what an RDS-managed secret produces,
# so the ":password::" selector in ecs.tf is unchanged if we ever adopt one.
#
# Note the absence of `ignore_changes = [secret_string]`, unlike the app secret
# in terraform/global/secrets.tf. There it permits out-of-band rotation; here it
# would let this secret drift from aws_rds_cluster.main.master_password, and
# every new task would crash-loop on a password the database never accepted.
resource "aws_secretsmanager_secret_version" "db" {
  secret_id = aws_secretsmanager_secret.db.id
  secret_string = jsonencode({
    username = var.db_username
    password = var.db_password
  })
}
