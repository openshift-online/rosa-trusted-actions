resource "aws_ebs_volume" "sqlite" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 20
  type              = "gp3"
  encrypted         = true

  tags = { Name = "${var.app_name}-sqlite", Environment = var.environment }

  # lifecycle {
  #   prevent_destroy = true # remove before Phase 2 teardown
  # }
}
