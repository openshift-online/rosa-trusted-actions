# Private Hosted Zone — VPC-scoped DNS for the internal FQDN.
#
# One PHZ is created per regional deployment. Every deployment uses the same
# zone name (var.internal_fqdn), giving callers an identical FQDN regardless
# of which region they are in. Route 53 Resolver inside each VPC answers
# queries using only the zones associated with that VPC, so resolution is
# always local — no cross-region DNS leakage is possible.
#
# This mirrors the Kubernetes svc.cluster.local model:
#   rosa-trusted-actions.internal.company.com  (same name everywhere)
#   → resolves to the regional internal ALB      (different target per VPC)
#
# See docs/networking.md for architecture diagrams.

resource "aws_route53_zone" "private" {
  count   = var.internal_fqdn != "" ? 1 : 0
  name    = var.internal_fqdn
  comment = "${var.app_name} private zone \u2014 ${var.aws_region} \u2014 scoped to VPC ${local.vpc_id}"

  vpc {
    vpc_id     = local.vpc_id
    vpc_region = var.aws_region
  }

  tags = { Environment = var.environment }

  # Prevent accidental destroy: changing the zone ID requires all consumers
  # to update their DNS configuration. Deletion must be intentional.
  lifecycle {
    prevent_destroy = true
  }
}

# Alias A record at the zone apex — the record ECS tasks and other VPC-internal
# clients actually resolve. Points to the internal (private) ALB for this region.
resource "aws_route53_record" "private_app" {
  count   = var.internal_fqdn != "" ? 1 : 0
  zone_id = aws_route53_zone.private[0].zone_id
  name    = var.internal_fqdn
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}
