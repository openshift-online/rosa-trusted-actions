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
  # setting carried by the file must NOT also appear in container_environment
  # — that is why the ROSA_TA_WORKER_* entries are gone: workers.* now lives
  # in the file only. The deprecated_* entries below are the one-release
  # compatibility shim: they re-add the env var only when the corresponding
  # deprecated variable is set, so an old tfvars value still applies (env
  # wins, exactly as before the migration).
  container_environment = concat([
    { name = "ROSA_TA_LISTEN_ADDR", value = ":8080" },
    { name = "ROSA_TA_LOG_JSON", value = "true" },
    { name = "ROSA_TA_LOG_LEVEL", value = "info" },
    { name = "ROSA_TA_ENABLE_AUTH", value = "true" },
    { name = "ROSA_TA_S3_BUCKET", value = aws_s3_bucket.app.bucket },
    { name = "ROSA_TA_S3_KEY_PREFIX", value = "trusted-actions" },
    { name = "ROSA_TA_OCM_BASE_URL", value = var.ocm_base_url },
    { name = "ROSA_TA_JWK_CERT_URL", value = var.jwk_cert_url },
    { name = "ROSA_TA_ALLOWED_ACCOUNTS", value = var.allowed_accounts },
    { name = "AWS_REGION", value = var.aws_region },
    { name = "DATABASE_URL", value = "/data/trusted_actions.db" },
    ],
    var.worker_concurrency != null ? [{ name = "ROSA_TA_WORKER_CONCURRENCY", value = tostring(var.worker_concurrency) }] : [],
    var.worker_poll_interval != null ? [{ name = "ROSA_TA_WORKER_POLL_INTERVAL", value = var.worker_poll_interval }] : [],
    var.worker_execution_timeout != null ? [{ name = "ROSA_TA_WORKER_EXECUTION_TIMEOUT", value = var.worker_execution_timeout }] : [],
    var.allowed_namespaces != null ? [{ name = "ROSA_TA_ALLOWED_NAMESPACES", value = var.allowed_namespaces }] : [],
    var.allowed_secrets != null ? [{ name = "ROSA_TA_ALLOWED_SECRETS", value = var.allowed_secrets }] : [],
    var.ocm_client_id != null ? [{ name = "ROSA_TA_OCM_CLIENT_ID", value = var.ocm_client_id }] : [],
    var.backplane_url != null ? [{ name = "ROSA_TA_BACKPLANE_URL", value = var.backplane_url }] : [],
    var.backplane_client_id != null ? [{ name = "ROSA_TA_BACKPLANE_CLIENT_ID", value = var.backplane_client_id }] : [],
  )

  # roles has no env var — the loader reads it from the file only. The shim
  # merges a deprecated var.roles into the config file when (and only when)
  # app_config does not define a roles block itself.
  app_config_effective = var.roles != null && try(length(yamldecode(var.app_config).roles), 1) == 0 ? yamlencode(merge(
    yamldecode(var.app_config),
    { roles = [for r in var.roles : { id = r.id, amsResource = r.ams_resource }] },
  )) : var.app_config

  container_secrets = [
    { name = "ROSA_TA_OCM_CLIENT_SECRET", valueFrom = "${local.global.secretsmanager_secret_arn}:ocm_client_secret::" },
    { name = "ROSA_TA_OCM_TOKEN", valueFrom = "${local.global.secretsmanager_secret_arn}:ocm_token::" },
    { name = "ROSA_TA_BACKPLANE_CLIENT_SECRET", valueFrom = "${local.global.secretsmanager_secret_arn}:backplane_client_secret::" },
  ]
}

resource "aws_s3_object" "config" {
  bucket       = aws_s3_bucket.app.bucket
  key          = "config/${var.aws_region}/config.yaml"
  content      = local.app_config_effective
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
