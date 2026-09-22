output "alb_dns_name" {
  description = "ALB DNS name — use as the API base URL"
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
