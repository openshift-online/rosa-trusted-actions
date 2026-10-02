resource "aws_ebs_volume" "sqlite" {
  availability_zone = data.aws_subnet.private_a.availability_zone
  size              = 20
  type              = "gp3"
  encrypted         = true

  tags = { Name = "${var.app_name}-sqlite", Environment = var.environment }
}
