# ECS Deployment: Phase 1 (EC2 + SQLite) and Phase 2 (Fargate + Postgres)

## Context

The ROSA Trusted Actions server is a Go binary (`rosa-trusted-actions-server`) running an HTTP API on `:8080` with an in-process goroutine worker pool synchronized via SQLite (`DATABASE_URL` env var). The store interface (`internal/store/Store`) is fully abstracted — `SQLiteStore` is one implementation; a `PostgresStore` will be the second.

Phase 1 creates all AWS infrastructure via Terraform (`terraform/`) to deploy the **current binary, unchanged**, as a single ECS EC2 task with an EBS-backed SQLite file. Phase 2 adds a `PostgresStore` implementation to the Go codebase (allowing the binary to detect a `postgres://` DSN at startup and route accordingly), then updates the Terraform to switch to Fargate with two tasks and an Aurora Serverless v2 cluster. Phase 2 Terraform is destructive to Phase 1 EC2/EBS resources — back up the SQLite file before applying.

Workers do not make AWS SDK calls. They call the Backplane HTTP API (HMAC-signed) and the Kubernetes API via the Backplane proxy. No worker-specific AWS credentials are needed; the app-level IAM task role covers only S3 and CloudWatch Metrics.

---

## Phase 1 — Terraform: ECS EC2 single-task with EBS-backed SQLite

All files live under `terraform/`. Non-secret values go in `terraform/terraform.tfvars`; secrets go in a separate `-var-file` kept **outside the repo** (e.g. `$HOME/.config/rosa-ta/secrets.tfvars`) so they're never committed — see [Running Phase 1](#running-phase-1) for the exact commands. No existing infra — all resources created from scratch.

### Step 1 — `terraform/versions.tf` — provider and backend skeleton

```hcl
terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Uncomment and configure for remote state:
  # backend "s3" {
  #   bucket         = "your-tfstate-bucket"
  #   key            = "rosa-trusted-actions/terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "terraform-locks"
  # }
}

provider "aws" {
  region = var.aws_region
}
```

### Step 2 — `terraform/variables.tf` — all input variables

```hcl
variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "app_name" {
  description = "Application name, used as prefix for all resource names"
  type        = string
  default     = "rosa-trusted-actions"
}

variable "environment" {
  description = "Deployment environment tag"
  type        = string
  default     = "prod"
}

variable "container_image" {
  description = "Full Quay.io image URI including tag — images are built by Tekton CI at quay.io/repository/redhat-user-workloads/rosa-tenant/rosa-trusted-actions."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for ECS host. t3.micro (1 GiB RAM) is sufficient for PoC with memory=512. Upgrade to t3.small (2 GiB) if workers are memory-hungry."
  type        = string
  default     = "t3.micro"
}

variable "domain_name" {
  description = "Public domain name for the API (e.g. trusted-actions.example.com). Required for ACM auto-provisioning. Leave empty to skip HTTPS."
  type        = string
  default     = ""
}

variable "route53_zone_id" {
  description = "Route53 hosted zone ID for var.domain_name. When set, ACM DNS validation records are created automatically. When empty (domain not in Route53), add the CNAME records manually and set var.alb_certificate_arn directly."
  type        = string
  default     = ""
}

variable "alb_certificate_arn" {
  description = "ACM certificate ARN for HTTPS listener. Populated automatically when var.domain_name + var.route53_zone_id are set (see acm.tf). Override manually if DNS is not in Route53."
  type        = string
  default     = ""
}

variable "s3_bucket_name" {
  description = "S3 bucket for execution outputs and logs (ROSA_TA_S3_BUCKET)"
  type        = string
}

variable "ocm_client_id" {
  description = "OCM client ID (ROSA_TA_OCM_CLIENT_ID)"
  type        = string
  default     = ""
}

variable "ocm_base_url" {
  description = "OCM base URL (ROSA_TA_OCM_BASE_URL)"
  type        = string
  default     = "https://api.openshift.com"
}

variable "jwk_cert_url" {
  description = "JWK certificate URL for JWT validation (ROSA_TA_JWK_CERT_URL)"
  type        = string
  default     = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/certs"
}

variable "backplane_url" {
  description = "Backplane API base URL (ROSA_TA_BACKPLANE_URL)"
  type        = string
}

variable "backplane_client_id" {
  description = "Backplane client ID for HMAC signing (ROSA_TA_BACKPLANE_CLIENT_ID)"
  type        = string
}

variable "allowed_accounts" {
  description = "Comma-separated AWS account IDs allowed to call the API (ROSA_TA_ALLOWED_ACCOUNTS)"
  type        = string
  default     = ""
}

variable "allowed_namespaces" {
  description = "Comma-separated Kubernetes namespaces allowed as action targets (ROSA_TA_ALLOWED_NAMESPACES)"
  type        = string
  default     = ""
}

variable "allowed_secrets" {
  description = "Comma-separated namespace/name pairs for allowed secrets (ROSA_TA_ALLOWED_SECRETS)"
  type        = string
  default     = ""
}

variable "worker_concurrency" {
  description = "Number of worker goroutines (ROSA_TA_WORKER_CONCURRENCY)"
  type        = number
  default     = 4
}

variable "worker_poll_interval" {
  description = "Worker poll interval, Go duration string (ROSA_TA_WORKER_POLL_INTERVAL)"
  type        = string
  default     = "5s"
}

variable "worker_execution_timeout" {
  description = "Max time per execution, Go duration string (ROSA_TA_WORKER_EXECUTION_TIMEOUT)"
  type        = string
  default     = "2m"
}

# Sensitive — store in tfvars or environment, never commit
variable "ocm_client_secret" {
  # TODO: ocm_client_secret and backplane_client_secret are separate Terraform
  # variables and Secrets Manager keys today. In practice both are the same
  # service-account credential. Merge into a single variable and secret key
  # when the app-level env vars are unified.
  description = "OCM client secret (ROSA_TA_OCM_CLIENT_SECRET)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "ocm_token" {
  description = "OCM offline token, alternative to client_secret (ROSA_TA_OCM_TOKEN)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "backplane_client_secret" {
  # TODO: see ocm_client_secret above — merge these once the app unifies its credential env vars.
  description = "Backplane HMAC signing secret (ROSA_TA_BACKPLANE_CLIENT_SECRET)"
  type        = string
  sensitive   = true
}

# Phase 2 only (unused in Phase 1)
variable "db_username" {
  description = "Aurora master username (Phase 2)"
  type        = string
  default     = "trusted_actions"
}

variable "db_name" {
  description = "Aurora database name (Phase 2)"
  type        = string
  default     = "trusted_actions"
}
```

### Step 3 — `terraform/outputs.tf` — useful outputs

```hcl
output "alb_dns_name" {
  description = "ALB DNS name — use as the API base URL"
  value       = aws_lb.main.dns_name
}

output "ecs_cluster_name" {
  description = "ECS cluster name"
  value       = aws_ecs_cluster.main.name
}

output "cloudwatch_log_group" {
  description = "CloudWatch log group name for container logs"
  value       = aws_cloudwatch_log_group.app.name
}
```

### Step 4 — `terraform/vpc.tf` — VPC, subnets, gateways

Two public subnets (ALB requires ≥2 AZs). One private subnet in AZ-a for the EC2 instance (AZ-a matches the EBS volume). NAT gateway for EC2 outbound to Backplane, OCM, S3. A second private subnet in AZ-b is created now but unused until Phase 2 Fargate.

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags = { Name = "${var.app_name}-vpc", Environment = var.environment }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.app_name}-igw" }
}

# Public subnets — ALB (two AZs required)
resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.0.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.app_name}-public-a" }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.app_name}-public-b" }
}

# Private subnet — EC2 instance (AZ-a, must match EBS volume AZ)
resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.10.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.app_name}-private-a" }
}

# Private subnet — Phase 2 Fargate second task (AZ-b); unused in Phase 1
resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.11.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "${var.app_name}-private-b" }
}

resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "${var.app_name}-nat-eip" }
}

resource "aws_nat_gateway" "main" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public_a.id
  tags          = { Name = "${var.app_name}-nat" }
  depends_on    = [aws_internet_gateway.main]
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = { Name = "${var.app_name}-public-rt" }
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main.id
  }
  tags = { Name = "${var.app_name}-private-rt" }
}

resource "aws_route_table_association" "public_a"  { subnet_id = aws_subnet.public_a.id;  route_table_id = aws_route_table.public.id }
resource "aws_route_table_association" "public_b"  { subnet_id = aws_subnet.public_b.id;  route_table_id = aws_route_table.public.id }
resource "aws_route_table_association" "private_a" { subnet_id = aws_subnet.private_a.id; route_table_id = aws_route_table.private.id }
resource "aws_route_table_association" "private_b" { subnet_id = aws_subnet.private_b.id; route_table_id = aws_route_table.private.id }
```

### Step 5 — `terraform/security_groups.tf` — ALB, EC2, and Phase 2 groups

`ecs_task_sg` and `aurora_sg` are created now (harmless) so Phase 2 doesn't require recreating the VPC-dependent groups mid-apply.

```hcl
resource "aws_security_group" "alb" {
  name        = "${var.app_name}-alb"
  description = "ALB: allow inbound HTTP/HTTPS from internet"
  vpc_id      = aws_vpc.main.id

  ingress { from_port = 80;   to_port = 80;   protocol = "tcp"; cidr_blocks = ["0.0.0.0/0"] }
  ingress { from_port = 443;  to_port = 443;  protocol = "tcp"; cidr_blocks = ["0.0.0.0/0"] }
  egress  { from_port = 8080; to_port = 8080; protocol = "tcp"; security_groups = [aws_security_group.ec2.id] }

  tags = { Name = "${var.app_name}-alb-sg" }
}

resource "aws_security_group" "ec2" {
  name        = "${var.app_name}-ec2"
  description = "ECS EC2 host: inbound from ALB, all outbound for backplane/OCM/S3"
  vpc_id      = aws_vpc.main.id

  ingress { from_port = 8080; to_port = 8080; protocol = "tcp"; security_groups = [aws_security_group.alb.id] }
  egress  { from_port = 0;    to_port = 0;    protocol = "-1";  cidr_blocks = ["0.0.0.0/0"] }

  tags = { Name = "${var.app_name}-ec2-sg" }
}

# Phase 2 — Fargate task SG (created now, wired in Phase 2 ecs.tf)
resource "aws_security_group" "ecs_task" {
  name        = "${var.app_name}-ecs-task"
  description = "Fargate task: inbound from ALB, outbound to Aurora and internet"
  vpc_id      = aws_vpc.main.id

  ingress { from_port = 8080; to_port = 8080; protocol = "tcp"; security_groups = [aws_security_group.alb.id] }
  egress  { from_port = 0;    to_port = 0;    protocol = "-1";  cidr_blocks = ["0.0.0.0/0"] }

  tags = { Name = "${var.app_name}-ecs-task-sg" }
}

# Phase 2 — Aurora SG (created now, referenced in aurora.tf)
resource "aws_security_group" "aurora" {
  name        = "${var.app_name}-aurora"
  description = "Aurora: inbound Postgres from Fargate tasks only"
  vpc_id      = aws_vpc.main.id

  ingress { from_port = 5432; to_port = 5432; protocol = "tcp"; security_groups = [aws_security_group.ecs_task.id] }

  tags = { Name = "${var.app_name}-aurora-sg" }
}
```

### Step 6 — `terraform/iam.tf` — three IAM roles

- **EC2 instance role**: registers host with ECS, SSM Session Manager access, `ec2:AttachVolume/DescribeVolumes` for EBS mount in user_data.
- **Task execution role**: ECR image pull, Secrets Manager injection at launch.
- **Task role**: S3 read/write on the output bucket, `cloudwatch:PutMetricData`.

```hcl
# ── EC2 Instance Role ──────────────────────────────────────────────────────────

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["ec2.amazonaws.com"] }
  }
}

resource "aws_iam_role" "ecs_instance" {
  name               = "${var.app_name}-ecs-instance"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

resource "aws_iam_role_policy_attachment" "ecs_instance_core" {
  role       = aws_iam_role.ecs_instance.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2ContainerServiceforEC2Role"
}

resource "aws_iam_role_policy_attachment" "ecs_instance_ssm" {
  role       = aws_iam_role.ecs_instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_policy" "ebs_attach" {
  name = "${var.app_name}-ebs-attach"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow"; Action = ["ec2:AttachVolume", "ec2:DescribeVolumes"]; Resource = "*" }]
  })
}

resource "aws_iam_role_policy_attachment" "ebs_attach" {
  role       = aws_iam_role.ecs_instance.name
  policy_arn = aws_iam_policy.ebs_attach.arn
}

resource "aws_iam_instance_profile" "ecs_instance" {
  name = "${var.app_name}-ecs-instance"
  role = aws_iam_role.ecs_instance.name
}

# ── ECS Task Execution Role ────────────────────────────────────────────────────

data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["ecs-tasks.amazonaws.com"] }
  }
}

resource "aws_iam_role" "task_execution" {
  name               = "${var.app_name}-task-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "task_execution_core" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_policy" "read_secrets" {
  name = "${var.app_name}-read-secrets"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      # Phase 2: add aws_secretsmanager_secret.db_url.arn here
      Resource = [aws_secretsmanager_secret.app.arn]
    }]
  })
}

resource "aws_iam_role_policy_attachment" "task_execution_secrets" {
  role       = aws_iam_role.task_execution.name
  policy_arn = aws_iam_policy.read_secrets.arn
}

# ── ECS Task Role (app permissions) ───────────────────────────────────────────

resource "aws_iam_role" "task" {
  name               = "${var.app_name}-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_policy" "task_app" {
  name = "${var.app_name}-task-app"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
        Resource = ["arn:aws:s3:::${var.s3_bucket_name}", "arn:aws:s3:::${var.s3_bucket_name}/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["cloudwatch:PutMetricData"]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "task_app" {
  role       = aws_iam_role.task.name
  policy_arn = aws_iam_policy.task_app.arn
}
```

### Step 7 — No ECR needed — images on Quay.io

Images are built by the existing Tekton CI pipeline and published to
`quay.io/repository/redhat-user-workloads/rosa-tenant/rosa-trusted-actions`.
The repository is public — ECS pulls without credentials, no `ecr.tf` or
`repositoryCredentials` needed. Set `var.container_image` in `terraform.tfvars`
to the full URI including tag.

### Step 8 — `terraform/acm.tf` — optional public TLS certificate (auto-provisioned)

AWS Certificate Manager issues public TLS certificates **for free** with no
manual CA interaction. The only requirement is proving domain ownership.

**If your domain is in Route53** (the common case for AWS-native setups), DNS
validation is fully automated — Terraform creates the CNAME records, waits for
AWS to validate them, and the certificate is ready in 1–5 minutes:

```hcl
resource "aws_acm_certificate" "app" {
  count             = var.domain_name != "" ? 1 : 0
  domain_name       = var.domain_name
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
  tags = { Environment = var.environment }
}

resource "aws_route53_record" "cert_validation" {
  for_each = var.domain_name != "" && var.route53_zone_id != "" ? {
    for dvo in aws_acm_certificate.app[0].domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  } : {}

  zone_id = var.route53_zone_id
  name    = each.value.name
  type    = each.value.type
  records = [each.value.record]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "app" {
  count                   = var.domain_name != "" && var.route53_zone_id != "" ? 1 : 0
  certificate_arn         = aws_acm_certificate.app[0].arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

locals {
  # The resolved certificate ARN: auto-provisioned takes precedence over manually supplied.
  certificate_arn = (
    var.domain_name != "" && var.route53_zone_id != ""
    ? aws_acm_certificate_validation.app[0].certificate_arn
    : var.alb_certificate_arn
  )
}
```

**If DNS is not in Route53**: skip `var.route53_zone_id`. Run
`terraform apply` once; ACM will display the required CNAME record. Add it to
your DNS provider manually. Once AWS detects it (minutes to hours depending on
TTL), the certificate validates. You can then set `var.alb_certificate_arn` to
the certificate ARN output and re-apply.

Either way, the ALB `https` listener references `local.certificate_arn`.
Without a domain name the HTTPS listener is simply omitted — HTTP:80 only.

### Step 9 — `terraform/cloudwatch.tf` — log group

**stdout is forwarded automatically.** The `awslogs` log driver in the ECS
task definition (Step 13) intercepts everything the process writes to stdout
and stderr and ships it to this log group. No code changes are needed — the
ECS agent handles the capture and delivery. The process just writes to stdout
as it does today.

Setting `ROSA_TA_LOG_JSON=true` makes Logrus emit structured JSON instead of
text. CloudWatch Logs Insights can then parse individual fields (`fields
@timestamp, status, execution_id`) for filtering and aggregation — without
it, each log line is an opaque string.

CloudWatch Metrics (`cloudwatch:PutMetricData`) is a separate, explicit API
call that the application would make via the AWS SDK when it is ready to emit
custom metrics. That is not yet wired up in the codebase; the IAM task role
grants the permission preemptively.

```hcl
resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/${var.app_name}"
  retention_in_days = 30
  tags              = { Environment = var.environment }
}
```

### Step 10 — `terraform/alb.tf` — Application Load Balancer

HTTP:80 always present. HTTPS:443 added when a certificate ARN is resolved (via `local.certificate_arn` from `acm.tf`). Health check targets `GET /health` (no auth, returns `{"status":"healthy"}`).

```hcl
resource "aws_lb" "main" {
  name               = var.app_name
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = [aws_subnet.public_a.id, aws_subnet.public_b.id]
  tags               = { Environment = var.environment }
}

resource "aws_lb_target_group" "app" {
  name        = var.app_name
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = aws_vpc.main.id
  target_type = "instance"  # Phase 1: EC2. Phase 2: change to "ip" for Fargate awsvpc

  health_check {
    path                = "/health"
    protocol            = "HTTP"
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Environment = var.environment }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = local.certificate_arn != "" ? "redirect" : "forward"

    dynamic "redirect" {
      for_each = local.certificate_arn != "" ? [1] : []
      content { port = "443"; protocol = "HTTPS"; status_code = "HTTP_301" }
    }

    dynamic "forward" {
      for_each = local.certificate_arn == "" ? [1] : []
      content { target_group { arn = aws_lb_target_group.app.arn } }
    }
  }
}

resource "aws_lb_listener" "https" {
  count             = local.certificate_arn != "" ? 1 : 0
  load_balancer_arn = aws_lb.main.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = local.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app.arn
  }
}
```

### Step 11 — `terraform/secrets.tf` — Secrets Manager for sensitive env vars

Three sensitive env vars stored as JSON keys in one secret. ECS injects each key as a separate env var via the `secretsmanager:arn:key::` valueFrom syntax.

> **TODO**: `ocm_client_secret` and `backplane_client_secret` are stored as separate keys today. In practice they are the same service-account credential. Merge into a single key when the application-level env vars are unified.

```hcl
resource "aws_secretsmanager_secret" "app" {
  name                    = "${var.app_name}/prod"
  description             = "Sensitive env vars for ${var.app_name}"
  recovery_window_in_days = 0  # immediate deletion; set to 30 for production
}

resource "aws_secretsmanager_secret_version" "app" {
  secret_id = aws_secretsmanager_secret.app.id
  secret_string = jsonencode({
    # TODO: merge ocm_client_secret and backplane_client_secret into one key once
    # ROSA_TA_OCM_CLIENT_SECRET and ROSA_TA_BACKPLANE_CLIENT_SECRET are unified.
    ocm_client_secret       = var.ocm_client_secret
    ocm_token               = var.ocm_token
    backplane_client_secret = var.backplane_client_secret
  })

  lifecycle {
    ignore_changes = [secret_string]  # allow out-of-band rotation without Terraform drift
  }
}
```

### Step 12 — `terraform/ebs.tf` — persistent SQLite EBS volume

gp3 20 GB, encrypted, in AZ-a (same as `private_a` subnet). `prevent_destroy = true` guards against accidental `terraform destroy` — remove this line explicitly before Phase 2 teardown.

```hcl
resource "aws_ebs_volume" "sqlite" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 20
  type              = "gp3"
  encrypted         = true

  tags = { Name = "${var.app_name}-sqlite", Environment = var.environment }

  lifecycle {
    prevent_destroy = true  # remove before Phase 2 teardown
  }
}
```

### Step 13 — `terraform/ec2.tf` — EC2 instance for ECS host

`aws_volume_attachment` attaches the EBS before user_data runs, so the script only needs to find and mount the device. On Nitro instances (t3, t4g), EBS volumes attached as `/dev/sdf` appear as `/dev/nvme1n1`; the user_data script handles both.

```hcl
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
    ignore_changes = [ami, user_data]  # prevent instance replacement on AMI updates
  }
}

resource "aws_volume_attachment" "sqlite" {
  device_name  = "/dev/sdf"
  volume_id    = aws_ebs_volume.sqlite.id
  instance_id  = aws_instance.ecs_host.id
  force_detach = false
}
```

`terraform/templates/userdata.sh.tpl`:

```bash
#!/bin/bash
set -euo pipefail

# Register with ECS cluster
echo "ECS_CLUSTER=${ecs_cluster_name}" >> /etc/ecs/ecs.config
echo "ECS_ENABLE_CONTAINER_METADATA=true" >> /etc/ecs/ecs.config

# Find the data volume device.
# Nitro instances: /dev/sdf attached by Terraform appears as /dev/nvme1n1 (second NVMe disk).
# Xen instances: appears as /dev/sdf or /dev/xvdf.
DATA_DEV=""
for i in $(seq 1 20); do
  for cand in /dev/nvme1n1 /dev/sdf /dev/xvdf; do
    if [ -b "$cand" ] && [ "$cand" != "/dev/nvme0n1" ]; then
      DATA_DEV="$cand"; break 2
    fi
  done
  sleep 3
done

if [ -z "$DATA_DEV" ]; then
  echo "ERROR: data volume not found after 60s" >&2; exit 1
fi

# Format only if no filesystem exists (idempotent across reboots)
if ! blkid "$DATA_DEV" > /dev/null 2>&1; then
  mkfs.ext4 -L ecs-data "$DATA_DEV"
fi

mkdir -p /mnt/ecs-data
mount "$DATA_DEV" /mnt/ecs-data
echo "LABEL=ecs-data /mnt/ecs-data ext4 defaults,nofail 0 2" >> /etc/fstab

# Container runs as UID 1001 (USER 1001 in Containerfile)
chmod 777 /mnt/ecs-data
```

### Step 14 — `terraform/ecs.tf` — ECS cluster, task definition, service (Phase 1)

The task definition binds host path `/mnt/ecs-data` → container path `/data`. `DATABASE_URL=/data/trusted_actions.db` — `NewSQLiteStore` appends WAL pragmas in code, no query params needed in the env var.

`deployment_minimum_healthy_percent = 0` is **required**: with a single EC2 instance, ECS cannot start the replacement task before stopping the old one. Without this the deployment deadlocks waiting for capacity that never appears.

**Memory sizing on t3.micro**: 1 GiB RAM total. ECS agent + OS overhead ~300 MB. Task `memory = 512` is the soft reservation ECS uses for scheduling decisions; the process typically runs well under 100 MB. If the task is OOM-killed, increase to 768.
```hcl
resource "aws_ecs_cluster" "main" {
  name = var.app_name
  setting { name = "containerInsights"; value = "enabled" }
  tags = { Environment = var.environment }
}

locals {
  container_environment = [
    { name = "ROSA_TA_LISTEN_ADDR",             value = ":8080" },
    { name = "ROSA_TA_LOG_JSON",                 value = "true" },
    { name = "ROSA_TA_LOG_LEVEL",                value = "info" },
    { name = "ROSA_TA_ENABLE_AUTH",              value = "true" },
    { name = "ROSA_TA_S3_BUCKET",                value = var.s3_bucket_name },
    { name = "ROSA_TA_S3_KEY_PREFIX",            value = "trusted-actions" },
    { name = "ROSA_TA_OCM_BASE_URL",             value = var.ocm_base_url },
    { name = "ROSA_TA_OCM_CLIENT_ID",            value = var.ocm_client_id },
    { name = "ROSA_TA_JWK_CERT_URL",             value = var.jwk_cert_url },
    { name = "ROSA_TA_BACKPLANE_URL",            value = var.backplane_url },
    { name = "ROSA_TA_BACKPLANE_CLIENT_ID",      value = var.backplane_client_id },
    { name = "ROSA_TA_ALLOWED_ACCOUNTS",         value = var.allowed_accounts },
    { name = "ROSA_TA_ALLOWED_NAMESPACES",       value = var.allowed_namespaces },
    { name = "ROSA_TA_ALLOWED_SECRETS",          value = var.allowed_secrets },
    { name = "ROSA_TA_WORKER_CONCURRENCY",       value = tostring(var.worker_concurrency) },
    { name = "ROSA_TA_WORKER_POLL_INTERVAL",     value = var.worker_poll_interval },
    { name = "ROSA_TA_WORKER_EXECUTION_TIMEOUT", value = var.worker_execution_timeout },
    { name = "AWS_REGION",                       value = var.aws_region },
    { name = "ROSA_TA_ROLES_CONFIG",             value = "configs/role_mapping.yaml" },
    { name = "DATABASE_URL",                     value = "/data/trusted_actions.db" },
    # Phase 2: remove DATABASE_URL from here; move to container_secrets
  ]

  container_secrets = [
    { name = "ROSA_TA_OCM_CLIENT_SECRET";       valueFrom = "${aws_secretsmanager_secret.app.arn}:ocm_client_secret::" },
    { name = "ROSA_TA_OCM_TOKEN";               valueFrom = "${aws_secretsmanager_secret.app.arn}:ocm_token::" },
    { name = "ROSA_TA_BACKPLANE_CLIENT_SECRET"; valueFrom = "${aws_secretsmanager_secret.app.arn}:backplane_client_secret::" },
    # Phase 2: add { name = "DATABASE_URL"; valueFrom = aws_secretsmanager_secret_version.db_url.arn }
  ]
}

resource "aws_ecs_task_definition" "app" {
  family             = var.app_name
  task_role_arn      = aws_iam_role.task.arn
  execution_role_arn = aws_iam_role.task_execution.arn
  network_mode       = "bridge"  # See bridge vs awsvpc note after this block

  # Phase 1: host volume binding for EBS-mounted SQLite. Remove for Phase 2.
  volume {
    name      = "sqlite-data"
    host_path = "/mnt/ecs-data"
  }

  container_definitions = jsonencode([{
    name      = var.app_name
    image     = var.container_image
    essential = true

    portMappings = [{ containerPort = 8080; hostPort = 8080; protocol = "tcp" }]

    # Phase 1 only — remove for Phase 2
    mountPoints = [{ sourceVolume = "sqlite-data"; containerPath = "/data"; readOnly = false }]

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
    memory = 512  # soft limit; fits t3.micro — raise to 768 if OOM-killed
  }])
}

resource "aws_ecs_service" "app" {
  name            = var.app_name
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.app.arn
  desired_count   = 1       # Phase 2: change to 2
  launch_type     = "EC2"   # Phase 2: change to "FARGATE"

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = var.app_name
    container_port   = 8080
  }

  ordered_placement_strategy { type = "binpack"; field = "cpu" }

  # 0/100: stop old task before starting new one (single EC2 instance, no spare capacity)
  # Phase 2: change to 100/200
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  depends_on = [aws_lb_listener.http, aws_iam_role_policy_attachment.task_execution_core]
}
```

**`network_mode = "bridge"` vs `"awsvpc"` — what changes when switching:**

In **bridge mode** (Phase 1) the container shares the EC2 host's network stack
via Docker's virtual bridge. Port mapping `hostPort = 8080` pins the host port,
so only one task can run per instance on that port. The ALB registers the EC2
*instance* as the target (`target_type = "instance"`), and traffic flows:
Internet → ALB → EC2 instance:8080 → container:8080. Security groups apply at
the EC2 instance level — every container on that host shares the same SG.

In **awsvpc mode** (Phase 2) each ECS task gets its own Elastic Network
Interface (ENI) with its own private IP address in the VPC. There is no port
mapping — the container listens on :8080 and the task ENI exposes that port
directly. The ALB registers individual task *IPs* as targets
(`target_type = "ip"`), and security groups apply per-task rather than
per-instance. This enables proper network isolation between tasks, is required
by Fargate, and is the only mode that supports per-task IAM credential
isolation via ECS task roles without the instance-level IAM role acting as a
bypass.

The concrete Terraform changes at Phase 2 migration:
- `aws_lb_target_group.app`: `target_type = "instance"` → `"ip"` (destroys/recreates the TG)
- `aws_ecs_task_definition.app`: `network_mode = "bridge"` → `"awsvpc"`, add `requires_compatibilities = ["FARGATE"]`, remove `portMappings.hostPort`
- `aws_ecs_service.app`: add `network_configuration` block with subnets and SG

---

## Phase 2A — Go: PostgresStore implementation

Add `github.com/jackc/pgx/v5` as a driver. Implement `PostgresStore` in a new file. Move the two shared where-clause builders to a shared file. Update `main.go` to detect the DSN prefix and select the store. No changes to the worker pool, handlers, or any other package.

### Step 15 — Add `pgx/v5` dependency

Run from the repo root:

```
go get github.com/jackc/pgx/v5@latest
```

Adds `github.com/jackc/pgx/v5` to `go.mod` as a direct dependency and updates `go.sum`. `sqlx` supports pgx via `pgx/v5/stdlib`, which registers the `"pgx"` driver name with `database/sql`.

### Step 15 — Create `internal/store/filters.go` — shared where-clause builders

Move `buildExecutionWhere` (lines 432–482 of `sqlite.go`) and `buildAuditWhere` (lines 484–514 of `sqlite.go`) verbatim into a new `internal/store/filters.go`. Delete them from `sqlite.go`. Both are unexported and only called within the `store` package. They reference `timeLayout`, which stays in `sqlite.go` — same package, accessible. The Postgres store calls `db.Rebind()` on the query string to convert `?` → `$N`.

### Step 16 — Create `internal/store/postgres_migrations/` SQL files

Separate from `internal/store/migrations/` (SQLite). Three migrations (vs four for SQLite — Postgres doesn't need the table-rebuild FK workaround from SQLite migration 003):

**`internal/store/postgres_migrations/001_create_tables.up.sql`:**

```sql
CREATE TABLE IF NOT EXISTS executions (
    id                 TEXT PRIMARY KEY,
    action             TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending',
    approval_state     TEXT,
    username           TEXT,
    target_cluster     TEXT NOT NULL,
    jira               TEXT,
    dry_run            BOOLEAN,
    force              BOOLEAN,
    params             TEXT,
    scope              TEXT,
    type               TEXT,
    revision           TEXT,
    manifest_work_name TEXT,
    output_path        TEXT,
    output_status      TEXT,
    runner_seconds     INTEGER,
    upload_seconds     INTEGER,
    duration_seconds   INTEGER,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    completed_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_executions_status             ON executions(status);
CREATE INDEX IF NOT EXISTS idx_executions_action             ON executions(action);
CREATE INDEX IF NOT EXISTS idx_executions_target_cluster     ON executions(target_cluster);
CREATE INDEX IF NOT EXISTS idx_executions_username           ON executions(username);
CREATE INDEX IF NOT EXISTS idx_executions_created_at         ON executions(created_at);
CREATE INDEX IF NOT EXISTS idx_executions_status_created_at  ON executions(status, created_at);

CREATE TABLE IF NOT EXISTS audit_entries (
    id              TEXT PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL,
    method          TEXT NOT NULL,
    path            TEXT NOT NULL,
    username        TEXT NOT NULL,
    status_code     INTEGER NOT NULL,
    action          TEXT,
    execution_id    TEXT REFERENCES executions(id),
    jira            TEXT,
    approval_state  TEXT,
    target_cluster  TEXT
);

CREATE INDEX IF NOT EXISTS idx_audit_entries_timestamp      ON audit_entries(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_entries_action         ON audit_entries(action);
CREATE INDEX IF NOT EXISTS idx_audit_entries_username       ON audit_entries(username);
CREATE INDEX IF NOT EXISTS idx_audit_entries_target_cluster ON audit_entries(target_cluster);
CREATE INDEX IF NOT EXISTS idx_audit_entries_method         ON audit_entries(method);
```

**`internal/store/postgres_migrations/001_create_tables.down.sql`:**

```sql
DROP TABLE IF EXISTS audit_entries;
DROP TABLE IF EXISTS executions;
```

### Step 17 — Create `internal/store/postgres_migrations.go`

Mirrors `migrations.go` but embeds `postgres_migrations/*.sql` and uses `$1`/`$2` placeholders directly (no Rebind needed — written explicitly for Postgres). Function names are `runPostgresMigrations` and `listPostgresMigrations` to avoid collision with the existing SQLite runner in the same package.

```go
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

//go:embed postgres_migrations/*.sql
var postgresMigrationsFS embed.FS

func runPostgresMigrations(ctx context.Context, db *sqlx.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	names, err := listPostgresMigrations(".up.sql")
	if err != nil {
		return err
	}

	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")

		var count int
		if err := db.GetContext(ctx, &count,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = $1", version); err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		content, err := postgresMigrationsFS.ReadFile("postgres_migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return errors.Join(fmt.Errorf("applying migration %s: %w", name, err), tx.Rollback())
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES ($1, $2)",
			version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return errors.Join(fmt.Errorf("recording migration %s: %w", name, err), tx.Rollback())
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", name, err)
		}
	}

	return nil
}

func listPostgresMigrations(suffix string) ([]string, error) {
	entries, err := postgresMigrationsFS.ReadDir("postgres_migrations")
	if err != nil {
		return nil, fmt.Errorf("reading postgres_migrations directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
```

### Step 18 — Create `internal/store/postgres.go`

Key differences from `SQLiteStore`:
- Driver: `"pgx"` (via `_ "github.com/jackc/pgx/v5/stdlib"`)
- No `SetMaxOpenConns(1)`; pool: 25 max open, 5 idle, 5min lifetime
- `pgExecutionRow` / `pgAuditRow` use `time.Time`/`*time.Time` directly — pgx handles `TIMESTAMPTZ` natively, no string parsing
- `ClaimNextExecution` uses `UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED) RETURNING id` — atomic, safe across any number of concurrent goroutines and server replicas
- All dynamically-built queries pass through `db.Rebind()` to convert `?` → `$N`

```go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/sirupsen/logrus"

	"github.com/openshift-online/rosa-trusted-actions/internal/models"
)

type PostgresStore struct {
	db     *sqlx.DB
	logger *logrus.Logger
}

var _ Store = (*PostgresStore)(nil)

func NewPostgresStore(ctx context.Context, dsn string, logger *logrus.Logger) (*PostgresStore, error) {
	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after ping failure")
		}
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	if err := runPostgresMigrations(ctx, db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.WithError(closeErr).Warn("Failed to close database after migration failure")
		}
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	logger.Info("Database initialized")
	return &PostgresStore{db: db, logger: logger}, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

// pgExecutionRow scans TIMESTAMPTZ columns as time.Time — no string parsing.
type pgExecutionRow struct {
	ID               string     `db:"id"`
	Action           string     `db:"action"`
	Status           string     `db:"status"`
	ApprovalState    *string    `db:"approval_state"`
	Username         *string    `db:"username"`
	TargetCluster    string     `db:"target_cluster"`
	Jira             *string    `db:"jira"`
	DryRun           *bool      `db:"dry_run"`
	Force            *bool      `db:"force"`
	Params           *string    `db:"params"`
	Scope            *string    `db:"scope"`
	Type             *string    `db:"type"`
	Revision         *string    `db:"revision"`
	ManifestWorkName *string    `db:"manifest_work_name"`
	OutputPath       *string    `db:"output_path"`
	OutputStatus     *string    `db:"output_status"`
	RunnerSeconds    *int       `db:"runner_seconds"`
	UploadSeconds    *int       `db:"upload_seconds"`
	DurationSeconds  *int       `db:"duration_seconds"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	CompletedAt      *time.Time `db:"completed_at"`
}

func (r *pgExecutionRow) toModel() (*models.Execution, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing execution id: %w", err)
	}
	exec := &models.Execution{
		ID: id, Action: r.Action, Status: r.Status,
		ApprovalState: r.ApprovalState, Username: r.Username, TargetCluster: r.TargetCluster,
		Jira: r.Jira, DryRun: r.DryRun, Force: r.Force, Scope: r.Scope,
		Type: r.Type, Revision: r.Revision, ManifestWorkName: r.ManifestWorkName,
		OutputPath: r.OutputPath, OutputStatus: r.OutputStatus,
		RunnerSeconds: r.RunnerSeconds, UploadSeconds: r.UploadSeconds, DurationSeconds: r.DurationSeconds,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, CompletedAt: r.CompletedAt,
	}
	if r.Params != nil {
		raw := json.RawMessage(*r.Params)
		exec.Params = &raw
	}
	return exec, nil
}

type pgAuditRow struct {
	ID            string    `db:"id"`
	Timestamp     time.Time `db:"timestamp"`
	Method        string    `db:"method"`
	Path          string    `db:"path"`
	Username      string    `db:"username"`
	StatusCode    int       `db:"status_code"`
	Action        *string   `db:"action"`
	ExecutionID   *string   `db:"execution_id"`
	Jira          *string   `db:"jira"`
	ApprovalState *string   `db:"approval_state"`
	TargetCluster *string   `db:"target_cluster"`
}

func (r *pgAuditRow) toModel() (*models.AuditEntry, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("parsing audit entry id: %w", err)
	}
	return &models.AuditEntry{
		ID: id, Timestamp: r.Timestamp, Method: r.Method, Path: r.Path,
		Username: r.Username, StatusCode: r.StatusCode, Action: r.Action,
		ExecutionID: r.ExecutionID, Jira: r.Jira,
		ApprovalState: r.ApprovalState, TargetCluster: r.TargetCluster,
	}, nil
}

func (s *PostgresStore) CreateExecution(ctx context.Context, exec *models.Execution) error {
	var paramsStr *string
	if exec.Params != nil {
		p := string(*exec.Params)
		paramsStr = &p
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO executions (
			id, action, status, approval_state, username, target_cluster,
			jira, dry_run, force, params, scope, type, revision,
			manifest_work_name, output_path, output_status,
			runner_seconds, upload_seconds, duration_seconds,
			created_at, updated_at, completed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22
		)`,
		exec.ID.String(), exec.Action, exec.Status, exec.ApprovalState, exec.Username, exec.TargetCluster,
		exec.Jira, exec.DryRun, exec.Force, paramsStr, exec.Scope, exec.Type, exec.Revision,
		exec.ManifestWorkName, exec.OutputPath, exec.OutputStatus,
		exec.RunnerSeconds, exec.UploadSeconds, exec.DurationSeconds,
		exec.CreatedAt.UTC(), exec.UpdatedAt.UTC(), exec.CompletedAt,
	)
	return wrapErr(err, "inserting execution")
}

func (s *PostgresStore) GetExecution(ctx context.Context, id uuid.UUID) (*models.Execution, error) {
	row := s.db.QueryRowxContext(ctx, "SELECT "+executionColumns+" FROM executions WHERE id = $1", id.String())
	var raw pgExecutionRow
	if err := row.StructScan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying execution: %w", err)
	}
	return raw.toModel()
}

func (s *PostgresStore) ListExecutions(ctx context.Context, filter ExecutionFilter) (*ExecutionListResult, error) {
	where, args := buildExecutionWhere(filter)
	whereClause := joinWhere(where)

	var total int
	if err := s.db.GetContext(ctx, &total,
		s.db.Rebind(fmt.Sprintf("SELECT COUNT(*) FROM executions %s", whereClause)),
		args...,
	); err != nil {
		return nil, fmt.Errorf("counting executions: %w", err)
	}

	limit, offset := clampPage(filter.Limit, filter.Offset)
	rows, err := s.db.QueryxContext(ctx,
		s.db.Rebind(fmt.Sprintf("SELECT %s FROM executions %s ORDER BY created_at DESC, id ASC LIMIT ? OFFSET ?",
			executionColumns, whereClause)),
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, fmt.Errorf("listing executions: %w", err)
	}
	defer rows.Close()

	var items []models.Execution
	for rows.Next() {
		var raw pgExecutionRow
		if err := rows.StructScan(&raw); err != nil {
			return nil, fmt.Errorf("scanning execution: %w", err)
		}
		exec, err := raw.toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, *exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating executions: %w", err)
	}
	return &ExecutionListResult{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *PostgresStore) UpdateExecutionStatus(ctx context.Context, id uuid.UUID, status string, completedAt *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE executions SET status = $1, updated_at = $2, completed_at = $3 WHERE id = $4",
		status, time.Now().UTC(), completedAt, id.String(),
	)
	return wrapErr(err, "updating execution status")
}

// ClaimNextExecution atomically claims the oldest pending execution.
// FOR UPDATE SKIP LOCKED is safe across concurrent goroutines and multiple server replicas.
func (s *PostgresStore) ClaimNextExecution(ctx context.Context) (*models.Execution, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.QueryRowContext(ctx, `
		UPDATE executions
		SET status = 'running', updated_at = NOW()
		WHERE id = (
			SELECT id FROM executions
			WHERE status = 'pending'
			ORDER BY created_at ASC, id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id`,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("claiming execution: %w", err)
	}

	row := tx.QueryRowxContext(ctx, "SELECT "+executionColumns+" FROM executions WHERE id = $1", id)
	var raw pgExecutionRow
	if err := row.StructScan(&raw); err != nil {
		return nil, fmt.Errorf("querying claimed execution: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim: %w", err)
	}
	return raw.toModel()
}

func (s *PostgresStore) CreateAuditEntry(ctx context.Context, entry *models.AuditEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_entries (
			id, timestamp, method, path, username, status_code,
			action, execution_id, jira, approval_state, target_cluster
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		entry.ID.String(), entry.Timestamp.UTC(),
		entry.Method, entry.Path, entry.Username, entry.StatusCode,
		entry.Action, entry.ExecutionID, entry.Jira, entry.ApprovalState, entry.TargetCluster,
	)
	return wrapErr(err, "inserting audit entry")
}

func (s *PostgresStore) ListAuditEntries(ctx context.Context, filter AuditFilter) (*AuditListResult, error) {
	where, args := buildAuditWhere(filter)
	whereClause := joinWhere(where)

	var total int
	if err := s.db.GetContext(ctx, &total,
		s.db.Rebind(fmt.Sprintf("SELECT COUNT(*) FROM audit_entries %s", whereClause)),
		args...,
	); err != nil {
		return nil, fmt.Errorf("counting audit entries: %w", err)
	}

	limit, offset := clampPage(filter.Limit, filter.Offset)
	rows, err := s.db.QueryxContext(ctx,
		s.db.Rebind(fmt.Sprintf("SELECT %s FROM audit_entries %s ORDER BY timestamp DESC, id ASC LIMIT ? OFFSET ?",
			auditColumns, whereClause)),
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, fmt.Errorf("listing audit entries: %w", err)
	}
	defer rows.Close()

	var items []models.AuditEntry
	for rows.Next() {
		var raw pgAuditRow
		if err := rows.StructScan(&raw); err != nil {
			return nil, fmt.Errorf("scanning audit entry: %w", err)
		}
		entry, err := raw.toModel()
		if err != nil {
			return nil, err
		}
		items = append(items, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit entries: %w", err)
	}
	return &AuditListResult{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func joinWhere(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(clauses, " AND ")
}

func clampPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return limit, offset
}

func wrapErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}
```

> **Note**: `joinWhere` and `clampPage` are small private helpers introduced in `postgres.go`. The identical limit/offset logic exists inline in `sqlite.go` — do not refactor `sqlite.go`; leave it as-is to keep the change minimal.

### Step 19 — Update `cmd/server/main.go` — DSN routing

Replace the single `store.NewSQLiteStore(...)` block (~line 90) with a DSN-prefix branch. Add `"strings"` to the import block if not already present.

Replace:
```go
dataStore, err := store.NewSQLiteStore(cmd.Context(), cfg.DatabaseURL, logger)
if err != nil {
    logger.WithError(err).Fatal("Failed to initialize database")
}
```

With:
```go
var dataStore store.Store
var dbErr error
if strings.HasPrefix(cfg.DatabaseURL, "postgres://") || strings.HasPrefix(cfg.DatabaseURL, "postgresql://") {
    dataStore, dbErr = store.NewPostgresStore(cmd.Context(), cfg.DatabaseURL, logger)
} else {
    dataStore, dbErr = store.NewSQLiteStore(cmd.Context(), cfg.DatabaseURL, logger)
}
if dbErr != nil {
    logger.WithError(dbErr).Fatal("Failed to initialize database")
}
```

Use `dbErr` (not `err`) to avoid shadowing the `err` variables declared later in `runServer` via `:=`.

### Step 20 — Add `internal/store/postgres_test.go` and update `integration/podman-compose.yml`

Mirror every test in `sqlite_test.go`. Skip when `POSTGRES_TEST_URL` env var is not set. Include a concurrent-claim test: 10 goroutines race `ClaimNextExecution` with 1 pending execution in the DB — assert exactly one wins and the rest return `ErrNotFound`. This validates `FOR UPDATE SKIP LOCKED`.

Add to `integration/podman-compose.yml`:

```yaml
  postgres:
    image: docker.io/library/postgres:16-alpine
    environment:
      POSTGRES_USER: trusted_actions
      POSTGRES_PASSWORD: trusted_actions
      POSTGRES_DB: trusted_actions
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "trusted_actions"]
      interval: 5s
      timeout: 3s
      retries: 10
```

Local test URL: `postgres://trusted_actions:trusted_actions@localhost:5432/trusted_actions?sslmode=disable`

---

## Phase 2B — Terraform: Fargate + Aurora Serverless v2

Apply after Phase 2A binary is in ECR. **Back up the SQLite file before applying** — Aurora starts empty.

```bash
aws ssm start-session --target <instance-id>
sqlite3 /mnt/ecs-data/trusted_actions.db ".backup /tmp/trusted_actions_backup.db"
aws s3 cp /tmp/trusted_actions_backup.db s3://<bucket>/backups/trusted_actions_$(date +%Y%m%d).db
```

### Step 21 — Create `terraform/aurora.tf`

```hcl
resource "random_password" "db" {
  length  = 32
  special = false  # Aurora master password disallows some special chars
}

resource "aws_db_subnet_group" "main" {
  name       = var.app_name
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  tags       = { Environment = var.environment }
}

resource "aws_rds_cluster" "main" {
  cluster_identifier        = var.app_name
  engine                    = "aurora-postgresql"
  engine_version            = "16.6"
  engine_mode               = "provisioned"
  database_name             = var.db_name
  master_username           = var.db_username
  master_password           = random_password.db.result
  db_subnet_group_name      = aws_db_subnet_group.main.name
  vpc_security_group_ids    = [aws_security_group.aurora.id]
  storage_encrypted         = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${var.app_name}-final"
  deletion_protection       = true

  serverlessv2_scaling_configuration {
    min_capacity = 0.5  # ~$43/mo at idle
    max_capacity = 4    # adjust upward if needed
  }
}

resource "aws_rds_cluster_instance" "writer" {
  identifier         = "${var.app_name}-writer"
  cluster_identifier = aws_rds_cluster.main.id
  instance_class     = "db.serverless"
  engine             = aws_rds_cluster.main.engine
  engine_version     = aws_rds_cluster.main.engine_version
}

output "aurora_endpoint" {
  description = "Aurora writer endpoint (internal)"
  value       = aws_rds_cluster.main.endpoint
  sensitive   = true
}
```

### Step 22 — Update `terraform/secrets.tf` — add DB URL secret

Append to `secrets.tf`:

```hcl
resource "aws_secretsmanager_secret" "db_url" {
  name                    = "${var.app_name}/db-url"
  description             = "Aurora connection URL for ${var.app_name}"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "db_url" {
  secret_id     = aws_secretsmanager_secret.db_url.id
  secret_string = "postgres://${var.db_username}:${random_password.db.result}@${aws_rds_cluster.main.endpoint}:5432/${var.db_name}?sslmode=require"
  depends_on    = [aws_rds_cluster.main]
}
```

In `iam.tf`, update the `read_secrets` policy `Resource` list to add `aws_secretsmanager_secret.db_url.arn`.

### Step 23 — Update `terraform/ecs.tf` for Fargate

Changes from Phase 1:
- `aws_lb_target_group.app`: `target_type = "instance"` → `"ip"` (required for awsvpc; destroys/recreates the TG — brief ALB downtime)
- Task definition: remove `volume` block, `network_mode = "awsvpc"`, add `requires_compatibilities = ["FARGATE"]`, `cpu`/`memory` at task level, remove `mountPoints`
- `container_environment`: remove `DATABASE_URL` entry
- `container_secrets`: add `{ name = "DATABASE_URL"; valueFrom = aws_secretsmanager_secret_version.db_url.arn }`
- Service: `desired_count = 2`, `launch_type = "FARGATE"`, add `network_configuration` block using `private_a` and `private_b` subnets with `ecs_task` SG, `deployment_minimum_healthy_percent = 100`, `deployment_maximum_percent = 200`

### Step 24 — Delete `terraform/ec2.tf` and `terraform/ebs.tf`

Remove `prevent_destroy` from `ebs.tf` first (or `terraform destroy -target=aws_ebs_volume.sqlite` will refuse). Delete both files. `terraform apply` then destroys `aws_instance.ecs_host`, `aws_volume_attachment.sqlite`, and `aws_ebs_volume.sqlite`. Also remove the `ebs_attach` IAM policy and its attachment from `iam.tf`.

---

## Running Phase 1

Secrets are kept out of `terraform/terraform.tfvars` (which is committed) and passed
via a second `-var-file` that lives outside the repo, so nothing sensitive ever
touches git:

```bash
cd terraform
terraform init

AWS_PROFILE=rosa terraform apply \
  -var-file=terraform.tfvars \
  -var-file=$HOME/.config/rosa-ta/secrets.tfvars
```

`terraform/terraform.tfvars` (committed, no secrets):

```hcl
container_image     = "quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:<digest>"
s3_bucket_name      = "rosa-trusted-actions"
backplane_url       = "https://fake.backplane.redhat.com"
backplane_client_id = "trusted-actions"
```

`$HOME/.config/rosa-ta/secrets.tfvars` (outside the repo, never committed —
placeholder values are fine for this PoC since backplane/OCM aren't wired to
real endpoints yet):

```hcl
backplane_client_secret = "empty"
ocm_client_secret       = "empty"
ocm_token               = "empty"
```

To deploy a new image build, update `container_image` in `terraform.tfvars`
to the new digest and re-run the `terraform apply` above — it updates the ECS
task definition and rolls the service.

---

## Terraform resource coverage

All resources are in `hashicorp/aws ~> 5.0` (stable):

| Resource | Notes |
|---|---|
| `aws_vpc`, subnets, IGW, NAT, routes | Core networking |
| `aws_security_group` | Phase 1 + Phase 2 groups created together |
| `aws_lb`, `aws_lb_target_group`, `aws_lb_listener` | ALB with optional HTTPS |
| `aws_ecs_cluster`, `aws_ecs_task_definition`, `aws_ecs_service` | EC2 launch type → Fargate |
| `aws_iam_role`, `aws_iam_policy`, `aws_iam_instance_profile` | Three roles |
| `aws_ecr_repository`, `aws_ecr_lifecycle_policy` | Container registry |
| `aws_cloudwatch_log_group` | Log retention |
| `aws_instance`, `aws_volume_attachment` | Phase 1 EC2 host |
| `aws_ebs_volume` | Phase 1 SQLite persistence |
| `aws_secretsmanager_secret`, `aws_secretsmanager_secret_version` | App secrets + DB URL |
| `aws_rds_cluster`, `aws_rds_cluster_instance`, `aws_db_subnet_group` | Aurora Serverless v2 |
| `random_password` | `hashicorp/random ~> 3.6` |
| `data.aws_ssm_parameter` | Latest ECS-optimized AMI |
| `data.aws_availability_zones` | AZ selection |

---

## Verification

### Phase 1 Terraform

Apply using both var-files as described in [Running Phase 1](#running-phase-1)
(images are built externally on Quay via Konflux/Tekton — no `docker push`
step here), then verify:

```bash
aws ecs list-tasks --cluster rosa-trusted-actions --query 'taskArns'
curl http://$(terraform output -raw alb_dns_name)/health
# Expected: {"status":"healthy","version":"..."}

# Confirm SQLite persists across task restart
aws ecs stop-task --cluster rosa-trusted-actions --task <task-arn>
# Wait ~60s, then:
curl http://$(terraform output -raw alb_dns_name)/health
```

### Phase 2A Go

```bash
cd integration && podman-compose up -d postgres
export POSTGRES_TEST_URL="postgres://trusted_actions:trusted_actions@localhost:5432/trusted_actions?sslmode=disable"
go test ./internal/store/... -v -run TestPostgres  # all pass
go test ./internal/store/... -v -run TestSQLite    # no regression

DATABASE_URL="$POSTGRES_TEST_URL" ROSA_TA_ENABLE_AUTH=false ROSA_TA_KUBECONFIG=~/.kube/config \
  go run ./cmd/server &
curl localhost:8080/health
psql "$POSTGRES_TEST_URL" -c "\dt"
# Expected tables: executions, audit_entries, schema_migrations
```

### Phase 2B Terraform

```bash
cd terraform
terraform plan   # review: EC2 + EBS destroyed, Fargate + Aurora created
terraform apply  # ~15 min

aws ecs list-tasks --cluster rosa-trusted-actions --query 'taskArns'
# Expected: 2 task ARNs

aws logs filter-log-events \
  --log-group-name /ecs/rosa-trusted-actions \
  --filter-pattern "Database initialized"
# Expected: 2 events (one per task)

curl http://$(terraform output -raw alb_dns_name)/health
```

---

## Assumptions

- **Container image architecture**: Tekton CI builds AMD64. Default `instance_type = "t3.medium"` targets x86-64. For ARM (t4g.medium, ~40% cheaper), change the SSM AMI path in `ec2.tf` to `/aws/service/ecs/optimized-ami/amazon-linux-2023/arm64/recommended/image_id` and build a multi-arch image.

- **`executionColumns` / `auditColumns` constants**: Defined in `sqlite.go`, referenced by `postgres.go`. Both files are in the `store` package — no relocation needed.

- **Aurora engine version**: Uses `16.6`. If unavailable in the target region, substitute the highest `16.x` from `aws rds describe-db-engine-versions --engine aurora-postgresql`.

- **`ROSA_TA_ROLES_CONFIG`**: Points to `/config/role_mapping.yaml`, populated at task startup by a non-essential `config-init` container (`public.ecr.aws/aws-cli/aws-cli`) that copies the file from S3 (uploaded by `terraform/config.tf` via `aws_s3_object`) into a shared, task-scoped `config-data` volume. The app container's `dependsOn` ensures it only starts once `config-init` exits successfully.

- **Data migration Phase 1 → Phase 2**: Aurora starts empty. Historical SQLite data is not migrated automatically. To migrate manually: `sqlite3` `.dump` → `psql` import with type casting (`TEXT` timestamps → `TIMESTAMPTZ`, `INTEGER` booleans → `BOOLEAN`).

- **`strings` import in `main.go`**: Not currently imported. Add it to the import block when making the DSN-routing change.
