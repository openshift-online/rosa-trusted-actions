# Regional ACM certificate for the internal FQDN.
#
# Each regional ALB requires its own certificate — ACM is a regional service.
# The DNS-01 validation CNAME that proves domain ownership is managed once in
# terraform/global/dns.tf and is already in place before this apply runs.
# aws_acm_certificate_validation waits for ACM to confirm ISSUED by polling
# ACM's API; it does not need to own the Route 53 record itself.

resource "aws_acm_certificate" "app" {
  count             = var.internal_fqdn != "" && var.public_zone_id != "" ? 1 : 0
  domain_name       = var.internal_fqdn
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
  tags = { Environment = var.environment }
}

resource "aws_acm_certificate_validation" "app" {
  count           = var.internal_fqdn != "" && var.public_zone_id != "" ? 1 : 0
  certificate_arn = aws_acm_certificate.app[0].arn

  # Derives the expected validation FQDN from the certificate itself. The actual
  # CNAME record is owned by terraform/global/dns.tf — this workspace never
  # touches the shared Route 53 record, so all regional applies can run in parallel.
  validation_record_fqdns = [
    for dvo in aws_acm_certificate.app[0].domain_validation_options : dvo.resource_record_name
  ]
}

locals {
  https_enabled = (var.internal_fqdn != "" && var.public_zone_id != "") || var.alb_certificate_arn != ""

  certificate_arn = (
    var.internal_fqdn != "" && var.public_zone_id != ""
    ? aws_acm_certificate_validation.app[0].certificate_arn
    : var.alb_certificate_arn
  )
}
