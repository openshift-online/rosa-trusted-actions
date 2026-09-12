resource "aws_ecs_cluster" "main" {
  name = var.app_name
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
  tags = { Environment = var.environment }
}

locals {
  container_environment = [
    { name = "ROSA_TA_LISTEN_ADDR", value = ":8080" },
    { name = "ROSA_TA_LOG_JSON", value = "true" },
    { name = "ROSA_TA_LOG_LEVEL", value = "info" },
    { name = "ROSA_TA_ENABLE_AUTH", value = "true" },
    { name = "ROSA_TA_S3_BUCKET", value = var.s3_bucket_name },
    { name = "ROSA_TA_S3_KEY_PREFIX", value = "trusted-actions" },
    { name = "ROSA_TA_OCM_BASE_URL", value = var.ocm_base_url },
    { name = "ROSA_TA_OCM_CLIENT_ID", value = var.ocm_client_id },
    { name = "ROSA_TA_JWK_CERT_URL", value = var.jwk_cert_url },
    { name = "ROSA_TA_BACKPLANE_URL", value = var.backplane_url },
    { name = "ROSA_TA_BACKPLANE_CLIENT_ID", value = var.backplane_client_id },
    { name = "ROSA_TA_ALLOWED_ACCOUNTS", value = var.allowed_accounts },
    { name = "ROSA_TA_ALLOWED_NAMESPACES", value = var.allowed_namespaces },
    { name = "ROSA_TA_ALLOWED_SECRETS", value = var.allowed_secrets },
    { name = "ROSA_TA_WORKER_CONCURRENCY", value = tostring(var.worker_concurrency) },
    { name = "ROSA_TA_WORKER_POLL_INTERVAL", value = var.worker_poll_interval },
    { name = "ROSA_TA_WORKER_EXECUTION_TIMEOUT", value = var.worker_execution_timeout },
    { name = "AWS_REGION", value = var.aws_region },
    { name = "ROSA_TA_ROLES_CONFIG", value = "/config/role_mapping.yaml" },
    { name = "DATABASE_URL", value = "/data/trusted_actions.db" },
    # Phase 2: remove DATABASE_URL from here; move to container_secrets
  ]

  container_secrets = [
    { name = "ROSA_TA_OCM_CLIENT_SECRET", valueFrom = "${aws_secretsmanager_secret.app.arn}:ocm_client_secret::" },
    { name = "ROSA_TA_OCM_TOKEN", valueFrom = "${aws_secretsmanager_secret.app.arn}:ocm_token::" },
    { name = "ROSA_TA_BACKPLANE_CLIENT_SECRET", valueFrom = "${aws_secretsmanager_secret.app.arn}:backplane_client_secret::" },
    # Phase 2: add { name = "DATABASE_URL"; valueFrom = aws_secretsmanager_secret_version.db_url.arn }
  ]
}

resource "aws_ecs_task_definition" "app" {
  family             = var.app_name
  task_role_arn      = aws_iam_role.task.arn
  execution_role_arn = aws_iam_role.task_execution.arn
  # bridge: container shares the EC2 host network via Docker bridge; hostPort pins 8080 on the host.
  # Phase 2: change to "awsvpc" — each task gets its own ENI and IP; ALB targets by IP, not instance.
  network_mode = "bridge"

  # Phase 1: host volume binding for EBS-mounted SQLite. Remove for Phase 2.
  volume {
    name      = "sqlite-data"
    host_path = "/mnt/ecs-data"
  }

  # Task-scoped, ephemeral — repopulated by config-init on every task start.
  volume {
    name = "config-data"
  }

  container_definitions = jsonencode([
    {
      name      = "config-init"
      image     = "public.ecr.aws/aws-cli/aws-cli:latest"
      essential = false

      command = ["s3", "cp", "s3://${var.s3_bucket_name}/config/role_mapping.yaml", "/config/role_mapping.yaml", "--region", var.aws_region]

      mountPoints = [{ sourceVolume = "config-data", containerPath = "/config", readOnly = false }]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.app.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs-config-init"
        }
      }

      cpu    = 256
      memory = 256
    },
    {
      name      = var.app_name
      image     = var.container_image
      essential = true

      dependsOn = [{ containerName = "config-init", condition = "SUCCESS" }]

      portMappings = [{ containerPort = 8080, hostPort = 8080, protocol = "tcp" }]

      mountPoints = [
        # Phase 1 only — remove for Phase 2
        { sourceVolume = "sqlite-data", containerPath = "/data", readOnly = false },
        { sourceVolume = "config-data", containerPath = "/config", readOnly = true },
      ]

      environment = local.container_environment
      secrets     = local.container_secrets

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.app.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs"
        }
      }

      # Soft limits used for scheduling. t3.micro has 1 GiB total; ~300 MB goes to OS + ECS agent.
      # 512 MB reservation leaves ~200 MB headroom. Raise to 768 if OOM-killed.
      cpu    = 512
      memory = 512
    }
  ])
}

resource "aws_ecs_service" "app" {
  name            = var.app_name
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 1     # Phase 2: change to 2
  launch_type     = "EC2" # Phase 2: change to "FARGATE"

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = var.app_name
    container_port   = 8080
  }

  ordered_placement_strategy {
    type  = "binpack"
    field = "cpu"
  }

  # 0/100: stop old task before starting new one.
  # Required with a single EC2 instance — no spare capacity to start the replacement first.
  # Phase 2: change to 100/200 (Fargate has capacity headroom).
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  depends_on = [aws_lb_listener.http, aws_iam_role_policy_attachment.task_execution_core]
}
