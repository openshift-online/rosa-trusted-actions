resource "aws_security_group" "alb" {
  name        = "${var.app_name}-alb"
  description = "ALB: allow inbound HTTP/HTTPS from within the VPC only"
  vpc_id      = local.vpc_id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.current.cidr_block]
  }
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.current.cidr_block]
  }

  tags = { Name = "${var.app_name}-alb-sg" }
}

resource "aws_security_group" "ecs_task" {
  name        = "${var.app_name}-ecs-task"
  description = "Fargate task: inbound from ALB, outbound to Aurora and internet"
  vpc_id      = local.vpc_id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.app_name}-ecs-task-sg" }
}

resource "aws_security_group" "aurora" {
  name        = "${var.app_name}-aurora"
  description = "Aurora: inbound Postgres from Fargate tasks only"
  vpc_id      = local.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs_task.id]
  }

  tags = { Name = "${var.app_name}-aurora-sg" }
}
