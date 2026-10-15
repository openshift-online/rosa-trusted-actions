# ACM DNS-01 validation CNAME — the only record ever written to the public zone.
#
# This resource lives in global/ because the CNAME is account-wide shared
# infrastructure, not per-region. ACM derives an identical validation CNAME for
# the same domain name regardless of which region the certificate was issued in,
# so a single record validates all regional certificates simultaneously.
#
# Moving it here eliminates the manage_validation_record flag that was previously
# required to prevent parallel regional applies from racing on the same Route 53
# record. Regional deployments now create their ACM certificates independently
# and wait for ACM to confirm ISSUED — which it will, because this CNAME already
# exists before any regional apply runs.
#
# See docs/networking.md for the full shadow-zone pattern explanation.

resource "aws_route53_record" "cert_validation" {
  for_each = var.internal_fqdn != "" && var.public_zone_id != "" ? {
    for dvo in aws_acm_certificate.validation_anchor[0].domain_validation_options : dvo.domain_name => {
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

# A minimal ACM certificate used only to obtain the validation CNAME token from
# ACM. It is never attached to an ALB. Each regional deployment issues its own
# certificate (see terraform/regional/acm.tf) and validates against this CNAME.
resource "aws_acm_certificate" "validation_anchor" {
  count             = var.internal_fqdn != "" && var.public_zone_id != "" ? 1 : 0
  domain_name       = var.internal_fqdn
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
  tags = { Purpose = "validation-anchor", Environment = var.environment }
}
