resource "aws_ecs_cluster" "main" {
  name = var.app_name
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
  tags = { Environment = var.environment }
}

locals {
  # app_config (see variables.tf) is the complete config file uploaded to S3.
  # Env vars override the config file silently in the Go loader, so any
  # setting carried by the file must NOT also appear below — that is why the
  # ROSA_TA_WORKER_* entries are gone: workers.* now lives in the file only.
  container_environment = [
    { name = "ROSA_TA_LISTEN_ADDR", value = ":8080" },
    { name = "ROSA_TA_LOG_JSON", value = "true" },
    { name = "ROSA_TA_LOG_LEVEL", value = "info" },
    { name = "ROSA_TA_ENABLE_AUTH", value = "true" },
    { name = "ROSA_TA_S3_BUCKET", value = aws_s3_bucket.app.bucket },
    { name = "ROSA_TA_S3_KEY_PREFIX", value = "trusted-actions" },
    { name = "ROSA_TA_OCM_BASE_URL", value = var.ocm_base_url },
    { name = "ROSA_TA_JWK_CERT_URL", value = var.jwk_cert_url },
    { name = "ROSA_TA_ALLOWED_ACCOUNTS", value = var.allowed_accounts },
    { name = "ROSA_TA_BACKPLANE_URL",  value = "https://api.stage.backplane.openshift.com" },
    { name = "AWS_REGION", value = var.aws_region },
    { name = "DATABASE_URL", value = "/data/trusted_actions.db" },
  ]

  container_secrets = [
    { name = "ROSA_TA_OCM_CLIENT_SECRET", valueFrom = "${local.global.secretsmanager_secret_arn}:ocm_client_secret::" },
    { name = "ROSA_TA_OCM_TOKEN", valueFrom = "${local.global.secretsmanager_secret_arn}:ocm_token::" },
    { name = "ROSA_TA_BACKPLANE_CLIENT_SECRET", valueFrom = "${local.global.secretsmanager_secret_arn}:backplane_client_secret::" },
  ]
}

resource "aws_s3_object" "config" {
  bucket       = aws_s3_bucket.app.bucket
  key          = "config/${var.aws_region}/config.yaml"
  content      = var.app_config
  content_type = "application/yaml"

  tags = { Environment = var.environment }
}

resource "aws_ecs_task_definition" "app" {
  family             = var.app_name
  task_role_arn      = local.global.iam_role_task_arn
  execution_role_arn = local.global.iam_role_task_execution_arn
  network_mode       = "bridge"

  volume {
    name      = "sqlite-data"
    host_path = "/mnt/ecs-data"
  }

  volume {
    name = "config-data"
  }

  container_definitions = jsonencode([
    {
      name      = "config-init"
      image     = "public.ecr.aws/aws-cli/aws-cli:latest"
      essential = false

      command = ["s3", "cp", "s3://${aws_s3_bucket.app.bucket}/${aws_s3_object.config.key}", "/config/config.yaml", "--region", var.aws_region]

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
      command   = ["--config-file", "/config/config.yaml"]

      dependsOn = [{ containerName = "config-init", condition = "SUCCESS" }]

      portMappings = [{ containerPort = 8080, hostPort = 8080, protocol = "tcp" }]

      mountPoints = [
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

      cpu    = 512
      memory = 512
    }
  ])
  depends_on = [
    aws_s3_object.config,
  ]
}

resource "aws_ecs_service" "app" {
  name                 = var.app_name
  cluster              = aws_ecs_cluster.main.id
  task_definition      = aws_ecs_task_definition.app.arn
  desired_count        = 1
  launch_type          = "EC2"
  force_new_deployment = true

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = var.app_name
    container_port   = 8080
  }

  ordered_placement_strategy {
    type  = "binpack"
    field = "cpu"
  }

  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  depends_on = [
    aws_lb_listener.http,
  ]
}
