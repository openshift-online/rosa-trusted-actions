resource "aws_acm_certificate" "app" {
  count             = var.domain_name != "" ? 1 : 0
  domain_name       = var.domain_name
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
  tags = { Environment = var.environment }
}

resource "aws_route53_record" "cert_validation" {
  for_each = var.domain_name != "" && var.route53_zone_id != "" ? {
    for dvo in aws_acm_certificate.app[0].domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  } : {}

  zone_id = var.route53_zone_id
  name    = each.value.name
  type    = each.value.type
  records = [each.value.record]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "app" {
  count                   = var.domain_name != "" && var.route53_zone_id != "" ? 1 : 0
  certificate_arn         = aws_acm_certificate.app[0].arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

resource "aws_route53_record" "app" {
  count   = var.domain_name != "" && var.route53_zone_id != "" ? 1 : 0
  zone_id = var.route53_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}

locals {
  # Known at plan time — depends only on variables, never on an unapplied resource's
  # attributes. Drives count/for_each in alb.tf; the real ARN is not known until apply.
  https_enabled = (var.domain_name != "" && var.route53_zone_id != "") || var.alb_certificate_arn != ""

  # Resolved certificate ARN: auto-provisioned (Route53) takes precedence over manually supplied.
  # Empty string when neither is set — HTTPS listener is omitted and ALB serves HTTP only.
  certificate_arn = (
    var.domain_name != "" && var.route53_zone_id != ""
    ? aws_acm_certificate_validation.app[0].certificate_arn
    : var.alb_certificate_arn
  )
}
