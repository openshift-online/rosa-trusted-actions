data "aws_ssm_parameter" "ecs_ami" {
  name = "/aws/service/ecs/optimized-ami/amazon-linux-2023/recommended/image_id"
}

# Looks up the AZ of an externally-provided private subnet so the EBS volume
# can be placed in the same AZ. Skipped when Terraform manages the VPC —
# the AZ is read directly from aws_subnet.private_a[0] in that case.
data "aws_subnet" "private_a" {
  count = local.create_vpc ? 0 : 1
  id    = var.private_subnet_ids[0]
}

resource "aws_instance" "ecs_host" {
  ami                    = data.aws_ssm_parameter.ecs_ami.value
  instance_type          = var.instance_type
  subnet_id              = local.private_subnet_a
  vpc_security_group_ids = [aws_security_group.ec2.id]
  iam_instance_profile   = local.global.iam_instance_profile_name

  user_data = templatefile("${path.module}/templates/userdata.sh.tpl", {
    ecs_cluster_name = aws_ecs_cluster.main.name
  })

  metadata_options {
    http_tokens = "required" # IMDSv2 mandatory
  }

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
