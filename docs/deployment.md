# Deployment: AWS Aurora / RDS Migration

## Overview

The ROSA Trusted Actions server currently uses SQLite (ECS EC2 + EBS). The
target state is AWS Aurora Serverless v2 (PostgreSQL-compatible), running on
ECS Fargate with two tasks. The `internal/store/Store` interface abstracts the
database backend — a `PostgresStore` implementation alongside the existing
`SQLiteStore` enables a DSN-prefix switch at startup (`postgres://` →
Postgres, anything else → SQLite).

All infrastructure is managed via Terraform (`terraform/`).

---

## Terraform Layout

```
terraform/
├── versions.tf         # Provider and backend configuration
├── variables.tf        # Input variables (region, image, secrets, etc.)
├── outputs.tf          # ALB DNS name, cluster name, log group
├── vpc.tf              # VPC, subnets, internet/NAT gateways, route tables
├── security_groups.tf  # ALB, EC2/Fargate task, and Aurora security groups
├── iam.tf              # EC2 instance role, task execution role, task role
├── acm.tf              # Optional ACM certificate (auto-provisioned via Route53)
├── cloudwatch.tf       # CloudWatch log group
├── alb.tf              # Application Load Balancer, target group, listeners
├── secrets.tf          # Secrets Manager entries for app secrets and DB URL
├── aurora.tf           # Aurora Serverless v2 cluster and writer instance
└── ecs.tf              # ECS cluster, Fargate task definition and service
```

Non-secret values go in `terraform/terraform.tfvars` (committed). Secrets go
in a separate var-file kept **outside the repo** (e.g.
`$HOME/.config/rosa-ta/secrets.tfvars`) so they are never committed.

---

## Applying the Terraform

```bash
cd terraform
terraform init

AWS_PROFILE=rosa terraform plan \
  -var-file=terraform.tfvars \
  -var-file=$HOME/.config/rosa-ta/secrets.tfvars

AWS_PROFILE=rosa terraform apply \
  -var-file=terraform.tfvars \
  -var-file=$HOME/.config/rosa-ta/secrets.tfvars
```

`terraform/terraform.tfvars` (no secrets):

```hcl
container_image     = "quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:<digest>"
s3_bucket_name      = "rosa-trusted-actions"
backplane_url       = "https://backplane.redhat.com"
backplane_client_id = "trusted-actions"
```

`$HOME/.config/rosa-ta/secrets.tfvars` (outside repo, never committed):

```hcl
backplane_client_secret = "<secret>"
ocm_client_secret       = "<secret>"
ocm_token               = "<token>"
```

To deploy a new image, update `container_image` in `terraform.tfvars` to the
new digest and re-run `terraform apply`.

---

## Verification

```bash
# Check tasks are running
aws ecs list-tasks --cluster rosa-trusted-actions --query 'taskArns'

# Health check via ALB
curl http://$(terraform output -raw alb_dns_name)/health
# Expected: {"status":"healthy","version":"..."}

# Confirm DB initialised in logs
aws logs filter-log-events \
  --log-group-name /ecs/rosa-trusted-actions \
  --filter-pattern "Database initialized"
```
