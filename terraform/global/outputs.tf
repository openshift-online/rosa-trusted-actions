output "s3_bucket_name" {
  description = "S3 bucket name — pass to regional workspaces as var.s3_bucket_name."
  value       = aws_s3_bucket.app.bucket
}

output "s3_bucket_arn" {
  description = "S3 bucket ARN."
  value       = aws_s3_bucket.app.arn
}

output "secretsmanager_secret_arn" {
  description = "Secrets Manager secret ARN — pass to regional workspaces as var.secretsmanager_secret_arn."
  value       = aws_secretsmanager_secret.app.arn
}

output "iam_role_task_arn" {
  description = "ECS task role ARN — pass to regional workspaces as var.iam_role_task_arn."
  value       = aws_iam_role.task.arn
}

output "iam_role_task_execution_arn" {
  description = "ECS task execution role ARN — pass to regional workspaces as var.iam_role_task_execution_arn."
  value       = aws_iam_role.task_execution.arn
}

output "iam_instance_profile_name" {
  description = "EC2 instance profile name — pass to regional workspaces as var.iam_instance_profile_name."
  value       = aws_iam_instance_profile.ecs_instance.name
}

output "iam_role_cwl_to_firehose_arn" {
  description = "CloudWatch Logs → Firehose role ARN — pass to regional workspaces as var.iam_role_cwl_to_firehose_arn."
  value       = aws_iam_role.cwl_to_firehose.arn
}

output "iam_role_firehose_audit_logs_arn" {
  description = "Firehose → S3 role ARN — pass to regional workspaces as var.iam_role_firehose_audit_logs_arn."
  value       = aws_iam_role.firehose_audit_logs.arn
}
