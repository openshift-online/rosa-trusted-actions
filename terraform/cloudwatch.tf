resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.app_name}"
  retention_in_days = 30
  tags              = { Environment = var.environment }
}

# Forwards only audit records (logrus "msg":"audit record" lines) to S3.
# See audit_logs.tf for the Firehose delivery stream and IAM roles.
resource "aws_cloudwatch_log_subscription_filter" "audit" {
  name            = "${var.app_name}-audit-to-firehose"
  log_group_name  = aws_cloudwatch_log_group.app.name
  filter_pattern  = "{ $.msg = \"audit record\" }"
  destination_arn = aws_kinesis_firehose_delivery_stream.audit_logs.arn
  role_arn        = aws_iam_role.cwl_to_firehose.arn

  # role_arn creates an implicit dep on the role resource but NOT on the policy
  # attachment. Without this, Terraform may create the filter before
  # firehose:PutRecord is attached, causing the CloudWatch test-message delivery
  # to fail with "Could not deliver test message to specified Firehose stream".
  depends_on = [aws_iam_role_policy_attachment.cwl_to_firehose]
}
