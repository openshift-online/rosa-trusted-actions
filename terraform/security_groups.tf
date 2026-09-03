resource "aws_security_group" "alb" {
  name        = "${var.app_name}-alb"
  description = "ALB: allow inbound HTTP/HTTPS from internet"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.app_name}-alb-sg" }
}

resource "aws_security_group" "ec2" {
  name        = "${var.app_name}-ec2"
  description = "ECS EC2 host: inbound from ALB, all outbound for backplane/OCM/S3"
  vpc_id      = aws_vpc.main.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.app_name}-ec2-sg" }
}

# alb <-> ec2 reference each other, so these rules are split out of the
# aws_security_group resources above to avoid a dependency cycle.
resource "aws_vpc_security_group_egress_rule" "alb_to_ec2" {
  security_group_id            = aws_security_group.alb.id
  referenced_security_group_id = aws_security_group.ec2.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "ec2_from_alb" {
  security_group_id            = aws_security_group.ec2.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

# Phase 2 — Fargate task SG (created now, wired in Phase 2 ecs.tf)
resource "aws_security_group" "ecs_task" {
  name        = "${var.app_name}-ecs-task"
  description = "Fargate task: inbound from ALB, outbound to Aurora and internet"
  vpc_id      = aws_vpc.main.id

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

# Phase 2 — Aurora SG (created now, referenced in aurora.tf)
resource "aws_security_group" "aurora" {
  name        = "${var.app_name}-aurora"
  description = "Aurora: inbound Postgres from Fargate tasks only"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.ecs_task.id]
  }

  tags = { Name = "${var.app_name}-aurora-sg" }
}
