resource "aws_ebs_volume" "sqlite" {
  availability_zone = local.private_subnet_a_az
  size              = 20
  type              = "gp3"
  encrypted         = true

  tags = { Name = "${var.app_name}-sqlite", Environment = var.environment }
}
