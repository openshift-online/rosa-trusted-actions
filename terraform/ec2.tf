data "aws_ssm_parameter" "ecs_ami" {
  # Amazon Linux 2023 ECS-optimized, x86_64 — change to arm64 path if using t4g
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/recommended/image_id"
}

resource "aws_instance" "ecs_host" {
  ami                    = data.aws_ssm_parameter.ecs_ami.value
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.private_a.id
  vpc_security_group_ids = [aws_security_group.ec2.id]
  iam_instance_profile   = aws_iam_instance_profile.ecs_instance.name

  user_data = base64encode(templatefile("${path.module}/templates/userdata.sh.tpl", {
    ecs_cluster_name = aws_ecs_cluster.main.name
  }))

  root_block_device {
    volume_type           = "gp3"
    volume_size           = 30
    delete_on_termination = true
    encrypted             = true
  }

  tags = { Name = "${var.app_name}-ecs-host", Environment = var.environment }

  lifecycle {
    ignore_changes = [ami, user_data] # prevent instance replacement on AMI updates; handle manually
  }
}

resource "aws_volume_attachment" "sqlite" {
  device_name  = "/dev/sdf"
  volume_id    = aws_ebs_volume.sqlite.id
  instance_id  = aws_instance.ecs_host.id
  force_detach = false
}
