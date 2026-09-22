resource "aws_s3_object" "role_mapping" {
  bucket = aws_s3_bucket.app.id
  key    = "config/role_mapping.yaml"
  source = "${path.module}/../configs/role_mapping.yaml"
  etag   = filemd5("${path.module}/../configs/role_mapping.yaml")
}
