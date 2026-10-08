output "internal_fqdn" {
  description = "Private FQDN for the API — resolvable only inside the associated VPC via the Route 53 Private Hosted Zone"
  value       = var.internal_fqdn != "" ? "https://${var.internal_fqdn}" : null
}

output "alb_dns_name" {
  description = "Internal ALB DNS name — prefer internal_fqdn for service-to-service calls; this is useful for health checks and debugging within the VPC"
  value       = aws_lb.main.dns_name
}

output "ecs_cluster_name" {
  description = "ECS cluster name"
  value       = aws_ecs_cluster.main.name
}

output "cloudwatch_log_group" {
  description = "CloudWatch log group name for container logs"
  value       = aws_cloudwatch_log_group.app.name
}

output "private_hosted_zone_id" {
  description = "Route 53 Private Hosted Zone ID for this regional deployment — use this when associating additional VPCs to the same zone"
  value       = var.internal_fqdn != "" ? aws_route53_zone.private[0].zone_id : null
}
