# Forwards only "audit record" log lines from aws_cloudwatch_log_group.app to
# S3 via a CloudWatch Logs subscription filter -> Kinesis Data Firehose.
# Firehose (not Kinesis Data Streams) is used deliberately: no per-shard-hour
# charge, so cost scales with the (currently very low) audit log volume.

data "aws_caller_identity" "current" {}

# ── CloudWatch Logs -> Firehose ───────────────────────────────────────────────

data "aws_iam_policy_document" "cwl_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["logs.${var.aws_region}.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      # Use the account-level wildcard from the AWS docs rather than the specific
      # log group ARN. The Terraform provider strips the trailing ":*" from log
      # group ARNs, so appending ":*" creates a 7-component pattern that never
      # matches the 6-component ARN CloudWatch Logs sends when assuming this role.
      values   = ["arn:aws:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:*"]
    }
  }
}

resource "aws_iam_role" "cwl_to_firehose" {
  name               = "${var.app_name}-cwl-to-firehose"
  assume_role_policy = data.aws_iam_policy_document.cwl_assume.json
}

resource "aws_iam_policy" "cwl_to_firehose" {
  name = "${var.app_name}-cwl-to-firehose"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["firehose:PutRecord", "firehose:PutRecordBatch"]
      Resource = aws_kinesis_firehose_delivery_stream.audit_logs.arn
    }]
  })
}

resource "aws_iam_role_policy_attachment" "cwl_to_firehose" {
  role       = aws_iam_role.cwl_to_firehose.name
  policy_arn = aws_iam_policy.cwl_to_firehose.arn
}

# ── Firehose -> S3 ─────────────────────────────────────────────────────────────

data "aws_iam_policy_document" "firehose_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["firehose.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = [data.aws_caller_identity.current.account_id]
    }
  }
}

resource "aws_iam_role" "firehose_audit_logs" {
  name               = "${var.app_name}-firehose-audit-logs"
  assume_role_policy = data.aws_iam_policy_document.firehose_assume.json
}

resource "aws_iam_policy" "firehose_audit_logs" {
  name = "${var.app_name}-firehose-audit-logs"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # Object-level permissions.
        # s3:PutObjectRetention is required even though the bucket has a default retention
        # rule: S3 still checks this permission when it applies the retention metadata
        # on behalf of the caller during PutObject. Without it, all Firehose writes are
        # rejected with AccessDenied when Object Lock is enabled on the bucket.
        Effect = "Allow"
        Action = ["s3:PutObject", "s3:PutObjectRetention", "s3:AbortMultipartUpload"]
        Resource = [
          "${aws_s3_bucket.app.arn}/audit-logs/*",
          "${aws_s3_bucket.app.arn}/audit-logs-errors/*",
        ]
      },
      {
        # Bucket-level permissions.
        # s3:GetBucketObjectLockConfiguration: Firehose checks this at stream
        # initialisation to determine whether to send retention headers.
        # s3:ListBucketMultipartUploads: Firehose uses this to clean up incomplete
        # multipart uploads after transient S3 errors.
        Effect = "Allow"
        Action = [
          "s3:GetBucketLocation",
          "s3:ListBucket",
          "s3:ListBucketMultipartUploads",
          "s3:GetBucketObjectLockConfiguration",
        ]
        Resource = [aws_s3_bucket.app.arn]
      },
      {
        Effect   = "Allow"
        Action   = ["logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.firehose_audit_logs.arn}:*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "firehose_audit_logs" {
  role       = aws_iam_role.firehose_audit_logs.name
  policy_arn = aws_iam_policy.firehose_audit_logs.arn
}

# Firehose's own delivery errors (e.g. S3 write failures), separate from the app log group.
resource "aws_cloudwatch_log_group" "firehose_audit_logs" {
  name              = "/aws/kinesisfirehose/${var.app_name}-audit-logs"
  retention_in_days = 30
  tags              = { Environment = var.environment }
}

resource "aws_cloudwatch_log_stream" "firehose_audit_logs" {
  name           = "S3Delivery"
  log_group_name = aws_cloudwatch_log_group.firehose_audit_logs.name
}

# ── Delivery stream ────────────────────────────────────────────────────────────

resource "aws_kinesis_firehose_delivery_stream" "audit_logs" {
  name        = "${var.app_name}-audit-logs"
  destination = "extended_s3"

  extended_s3_configuration {
    role_arn   = aws_iam_role.firehose_audit_logs.arn
    bucket_arn = aws_s3_bucket.app.arn
    prefix     = "audit-logs/"

    error_output_prefix = "audit-logs-errors/!{firehose:error-output-type}/"

    # Default buffer (5 MiB / 300s); at current audit log volume the time
    # threshold will flush first regardless of the size limit.
    buffering_size     = 5
    buffering_interval = 300
    compression_format = "GZIP"

    cloudwatch_logging_options {
      enabled         = true
      log_group_name  = aws_cloudwatch_log_group.firehose_audit_logs.name
      log_stream_name = aws_cloudwatch_log_stream.firehose_audit_logs.name
    }
  }

  # The role ARN reference above creates an implicit dependency on the role resource,
  # but NOT on the policy attachment. Without this, Firehose validates S3 connectivity
  # before the policy is attached, fails to write, and never reaches ACTIVE state —
  # which then causes the CloudWatch subscription filter to fail with
  # "Could not deliver test message to specified Firehose stream".
  # The bucket_policy dep ensures the DenyNonTLS policy is in place before Firehose
  # makes its first test write.
  depends_on = [
    aws_iam_role_policy_attachment.firehose_audit_logs,
    aws_s3_bucket_policy.app,
  ]
}
