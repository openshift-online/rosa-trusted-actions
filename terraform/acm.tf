# ACM certificate for the internal FQDN.
#
# Shadow-zone pattern: the certificate covers var.internal_fqdn, which is a
# subdomain of a real public domain you control. ACM validates ownership via a
# DNS-01 CNAME placed in the PUBLIC Route 53 zone (var.public_zone_id).
# No A record is ever created in the public zone, so the FQDN is publicly
# unresolvable (NXDOMAIN). The actual A alias record lives in the Private
# Hosted Zone (route53-private.tf) and is only visible inside the associated VPC.
#
# Multi-region parallelism
# ------------------------
# Every regional deployment issues its own ACM certificate (ACM is a regional
# service; an ALB in eu-west-1 cannot use a certificate from us-east-1).
# However, ACM derives an IDENTICAL validation CNAME for the same domain name
# regardless of region, so the CNAME is effectively global shared infrastructure.
#
# To allow regional Terraform applies to run in parallel, exactly ONE regional
# deployment must own the Route 53 validation record (var.manage_validation_record
# = true). All other regions set manage_validation_record = false: they create
# their own ACM certificate, wait for ACM to confirm it is ISSUED (which happens
# automatically once the owning region's CNAME is in place), and never touch the
# shared Route 53 record. See docs/networking.md for a deployment order diagram.
#
# See docs/networking.md for a full explanation and diagrams.

resource "aws_acm_certificate" "app" {
  count             = var.internal_fqdn != "" && var.public_zone_id != "" ? 1 : 0
  domain_name       = var.internal_fqdn
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
  tags = { Environment = var.environment }
}

# CNAME placed in the PUBLIC zone for ACM DNS-01 validation.
#
# Created only by the deployment with manage_validation_record = true (the
# designated record owner, typically the primary region). All other regional
# deployments skip this resource entirely — the CNAME is already in place and
# ACM validates each region's certificate against it independently.
resource "aws_route53_record" "cert_validation" {
  for_each = var.internal_fqdn != "" && var.public_zone_id != "" && var.manage_validation_record ? {
    for dvo in aws_acm_certificate.app[0].domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  } : {}

  zone_id = var.public_zone_id
  name    = each.value.name
  type    = each.value.type
  records = [each.value.record]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "app" {
  count           = var.internal_fqdn != "" && var.public_zone_id != "" ? 1 : 0
  certificate_arn = aws_acm_certificate.app[0].arn

  # Derive the expected validation FQDN from the certificate's own
  # domain_validation_options rather than from aws_route53_record.cert_validation.
  # This decouples the Terraform dependency graph: non-owning regions do not hold
  # a reference to the Route 53 record resource (which lives in a different
  # workspace's state) and can therefore apply in parallel. The wait is still
  # correct — Terraform polls ACM's API until the certificate status is ISSUED,
  # which ACM sets once it resolves the CNAME that the owning region created.
  validation_record_fqdns = [
    for dvo in aws_acm_certificate.app[0].domain_validation_options : dvo.resource_record_name
  ]
}

locals {
  https_enabled = (var.internal_fqdn != "" && var.public_zone_id != "") || var.alb_certificate_arn != ""

  # Resolved certificate ARN: auto-provisioned (Route 53 DNS-01) takes precedence
  # over manually supplied. Empty string when neither is set — HTTPS listener is
  # omitted and ALB serves HTTP only.
  certificate_arn = (
    var.internal_fqdn != "" && var.public_zone_id != ""
    ? aws_acm_certificate_validation.app[0].certificate_arn
    : var.alb_certificate_arn
  )
}
