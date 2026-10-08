# Forwards "audit record" log lines from the ECS log group to S3 via
# CloudWatch Logs subscription filter → Kinesis Data Firehose.

data "aws_caller_identity" "current" {}

# ── CloudWatch Logs -> Firehose ───────────────────────────────────────────────

resource "aws_cloudwatch_log_subscription_filter" "audit" {
  name            = "${var.app_name}-audit-to-firehose"
  log_group_name  = aws_cloudwatch_log_group.app.name
  filter_pattern  = "{ $.msg = \"audit record\" }"
  destination_arn = aws_kinesis_firehose_delivery_stream.audit_logs.arn
  role_arn        = local.global.iam_role_cwl_to_firehose_arn

  depends_on = [aws_kinesis_firehose_delivery_stream.audit_logs]
}

# ── Firehose error log group ──────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "firehose_audit_logs" {
  name              = "/aws/kinesisfirehose/${var.app_name}-audit-logs"
  retention_in_days = 30
  tags              = { Environment = var.environment }
}

resource "aws_cloudwatch_log_stream" "firehose_audit_logs" {
  name           = "S3Delivery"
  log_group_name = aws_cloudwatch_log_group.firehose_audit_logs.name
}

# ── Firehose -> S3 ────────────────────────────────────────────────────────────

resource "aws_kinesis_firehose_delivery_stream" "audit_logs" {
  name        = "${var.app_name}-audit-logs"
  destination = "extended_s3"

  extended_s3_configuration {
    role_arn   = local.global.iam_role_firehose_audit_logs_arn
    bucket_arn = local.global.s3_bucket_arn
    prefix     = "audit-logs/"

    error_output_prefix = "audit-logs-errors/!{firehose:error-output-type}/"

    buffering_size     = 5
    buffering_interval = 300
    compression_format = "GZIP"

    cloudwatch_logging_options {
      enabled         = true
      log_group_name  = aws_cloudwatch_log_group.firehose_audit_logs.name
      log_stream_name = aws_cloudwatch_log_stream.firehose_audit_logs.name
    }
  }
}
