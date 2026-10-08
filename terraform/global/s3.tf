data "aws_caller_identity" "current" {}

# ── S3 bucket ─────────────────────────────────────────────────────────────────

# Object lock must be enabled at bucket creation — it cannot be added to an
# existing bucket without an AWS Support request.
resource "aws_s3_bucket" "app" {
  bucket              = var.s3_bucket_name
  object_lock_enabled = true

  tags = { Name = var.s3_bucket_name, Environment = var.environment }
}

resource "aws_s3_bucket_versioning" "app" {
  bucket = aws_s3_bucket.app.id

  versioning_configuration {
    status = "Enabled"
  }
}

# WORM enforcement — COMPLIANCE mode prevents deletion or modification of
# objects for the full retention period, even by the bucket owner.
resource "aws_s3_bucket_object_lock_configuration" "app" {
  count  = var.enable_worm ? 1 : 0
  bucket = aws_s3_bucket.app.id

  rule {
    default_retention {
      mode = "COMPLIANCE"
      days = var.retention_days
    }
  }

  depends_on = [aws_s3_bucket_versioning.app]
}

resource "aws_s3_bucket_public_access_block" "app" {
  bucket                  = aws_s3_bucket.app.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "app" {
  bucket = aws_s3_bucket.app.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_policy" "app" {
  bucket = aws_s3_bucket.app.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "DenyNonTLS"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource = [
        aws_s3_bucket.app.arn,
        "${aws_s3_bucket.app.arn}/*",
      ]
      Condition = {
        Bool = { "aws:SecureTransport" = "false" }
      }
    }]
  })
}

resource "aws_s3_bucket_lifecycle_configuration" "audit_logs" {
  bucket = aws_s3_bucket.app.id

  rule {
    id     = "audit-logs-transition"
    status = "Enabled"

    filter {
      prefix = "audit-logs/"
    }

    transition {
      days          = 30
      storage_class = "STANDARD_IA"
    }

    transition {
      days          = 90
      storage_class = "GLACIER"
    }

    expiration {
      expired_object_delete_marker = true
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

resource "aws_s3_object" "role_mapping" {
  bucket = aws_s3_bucket.app.id
  key    = "config/role_mapping.yaml"
  source = "${path.module}/../../configs/role_mapping.yaml"
  etag   = filemd5("${path.module}/../../configs/role_mapping.yaml")
}
