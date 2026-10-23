# All IAM resources are account-global; role and policy names must be unique
# per account regardless of region.
#
# Three policies reference resources that are per-regional-deployment:
#
#   cwl_to_firehose   — grants PutRecord on the Firehose delivery stream.
#                       Wildcarded to arn:aws:firehose:*:ACCOUNT:deliverystream/APP_NAME-audit-logs
#                       so the same role works in every region.
#
#   firehose_audit_logs — grants PutLogEvents on the Firehose error log group.
#                         Wildcarded to arn:aws:logs:*:ACCOUNT:log-group:/aws/kinesisfirehose/APP_NAME-audit-logs:*
#
#   read_secrets — grants GetSecretValue on the Aurora credentials secret.
#                 Wildcarded to arn:aws:secretsmanager:*:ACCOUNT:secret:APP_NAME/db-*
#                 because the secret is created in the regional workspace, which
#                 this global workspace never observes, and Secrets Manager
#                 appends a random six-character suffix to the name.
#
# The wildcard scope is narrow: it only covers the predictable resource names
# this project creates and is further constrained by the Firehose / CWL service
# principal trust policies.

# ── ECS Task Execution Role ────────────────────────────────────────────────────

data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "task_execution" {
  name               = "${var.app_name}-task-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "task_execution_core" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_policy" "read_secrets" {
  name = "${var.app_name}-read-secrets"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["secretsmanager:GetSecretValue"]
      Resource = [
        aws_secretsmanager_secret.app.arn,
        # The Aurora credentials secret is created in the regional root, which is
        # applied after this one, so its full ARN is not knowable here. The name
        # is ours and predictable; the trailing -* covers the random suffix
        # Secrets Manager appends. See docs/database-and-compute.md.
        "arn:aws:secretsmanager:*:${data.aws_caller_identity.current.account_id}:secret:${var.app_name}/db-*",
      ]
    }]
  })
}

resource "aws_iam_role_policy_attachment" "task_execution_secrets" {
  role       = aws_iam_role.task_execution.name
  policy_arn = aws_iam_policy.read_secrets.arn
}

# ── ECS Task Role (app permissions) ───────────────────────────────────────────

resource "aws_iam_role" "task" {
  name               = "${var.app_name}-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_policy" "task_app" {
  name = "${var.app_name}-task-app"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:ListBucket"]
        Resource = ["arn:aws:s3:::${var.s3_bucket_name}", "arn:aws:s3:::${var.s3_bucket_name}/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:PutObject"]
        Resource = ["arn:aws:s3:::${var.s3_bucket_name}/trusted-actions/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["cloudwatch:PutMetricData"]
        Resource = "*"
      },
      {
        # Required for ECS Exec — the SSM data channel is opened by the task
        # role, not the task execution role.
        Effect = "Allow"
        Action = [
          "ssmmessages:CreateControlChannel",
          "ssmmessages:CreateDataChannel",
          "ssmmessages:OpenControlChannel",
          "ssmmessages:OpenDataChannel",
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "task_app" {
  role       = aws_iam_role.task.name
  policy_arn = aws_iam_policy.task_app.arn
}

# ── CloudWatch Logs -> Firehose role ──────────────────────────────────────────
#
# Service principal: logs.amazonaws.com (without region) so the same role can
# be assumed by CloudWatch Logs in any region. The SourceArn condition restricts
# assumption to log groups in this account across all regions.

data "aws_iam_policy_document" "cwl_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["logs.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [data.aws_caller_identity.current.account_id]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:aws:logs:*:${data.aws_caller_identity.current.account_id}:*"]
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
      Effect = "Allow"
      Action = ["firehose:PutRecord", "firehose:PutRecordBatch"]
      # Wildcarded across regions — every regional deployment creates a Firehose
      # stream with this predictable name.
      Resource = ["arn:aws:firehose:*:${data.aws_caller_identity.current.account_id}:deliverystream/${var.app_name}-audit-logs"]
    }]
  })
}

resource "aws_iam_role_policy_attachment" "cwl_to_firehose" {
  role       = aws_iam_role.cwl_to_firehose.name
  policy_arn = aws_iam_policy.cwl_to_firehose.arn
}

# ── Firehose -> S3 role ────────────────────────────────────────────────────────

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
        Effect = "Allow"
        Action = ["s3:PutObject", "s3:PutObjectRetention", "s3:AbortMultipartUpload"]
        Resource = [
          "${aws_s3_bucket.app.arn}/audit-logs/*",
          "${aws_s3_bucket.app.arn}/audit-logs-errors/*",
        ]
      },
      {
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
        Effect = "Allow"
        Action = ["logs:PutLogEvents"]
        # Wildcarded across regions — every regional deployment creates this log
        # group with the same predictable name.
        Resource = ["arn:aws:logs:*:${data.aws_caller_identity.current.account_id}:log-group:/aws/kinesisfirehose/${var.app_name}-audit-logs:*"]
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "firehose_audit_logs" {
  role       = aws_iam_role.firehose_audit_logs.name
  policy_arn = aws_iam_policy.firehose_audit_logs.arn
}
