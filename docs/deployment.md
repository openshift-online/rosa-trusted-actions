# Deployment

## Overview

Infrastructure is split into two Terraform roots that must be applied in order:

| Root         | Path                  | Scope                                                                                                      | Apply cadence    |
|--------------|-----------------------|------------------------------------------------------------------------------------------------------------|------------------|
| **global**   | `terraform/global/`   | Account-wide: S3, IAM, Secrets Manager, ACM validation CNAME                                               | Once per account |
| **regional** | `terraform/regional/` | One deployment per AWS region: VPC, ALB, ECS, EC2, EBS, CloudWatch, Route 53 PHZ, regional ACM certificate | Once per region  |

The regional root reads IAM ARNs, the S3 bucket name, and the Secrets Manager
ARN directly from the global workspace's Terraform state via
`terraform_remote_state`. No values need to be copied by hand.

See [docs/networking.md](networking.md) for the DNS and TLS architecture.

---

## Table of Contents

1. [HCP Terraform (CI/CD)](#hcp-terraform-cicd)
2. [Local Testing](#local-testing)
3. [Verification](#verification)
4. [Adding a New Region](#adding-a-new-region)

---

## HCP Terraform (CI/CD)

Workspaces are managed in `hcp-terraform/rosa-trusted-actions/main.tf`.
Apply that configuration first to create the workspaces, then use the HCP
Terraform UI or CLI to trigger runs.

### Workspace summary

| Workspace                     | Working directory    | Must apply before       |
|-------------------------------|----------------------|-------------------------|
| `rosa-trusted-actions-global` | `terraform/global`   | all regional workspaces |
| `rosa-trusted-actions-stage`  | `terraform/regional` | —                       |

### Apply order

```
1. rosa-trusted-actions-global   (creates S3, IAM, Secrets, CNAME)
         │
         └─ run trigger fires automatically
                  ↓
2. rosa-trusted-actions-stage    (reads global outputs via remote state)
```

A `tfe_workspace_run_trigger` queues the regional run automatically after a
successful global apply. Each regional workspace is also granted read access
to the global workspace's state via `tfe_workspace_settings` — no extra
configuration needed.

### Workspace variables

**Global workspace** (`rosa-trusted-actions-global`):

| Variable                  | Sensitive | Notes                                            |
|---------------------------|-----------|--------------------------------------------------|
| `aws_region`              | no        | Provider region, e.g. `us-east-1`                |
| `environment`             | no        | `stage` or `prod`                                |
| `s3_bucket_name`          | no        | Globally unique bucket name                      |
| `internal_fqdn`           | no        | e.g. `rosa-trusted-actions.internal.company.com` |
| `public_zone_id`          | no        | Route 53 public zone ID (add when ready for TLS) |
| `backplane_client_secret` | **yes**   |                                                  |
| `ocm_client_secret`       | **yes**   |                                                  |
| `ocm_token`               | **yes**   |                                                  |

**Regional workspace** (`rosa-trusted-actions-stage`):

| Variable              | Sensitive | Notes                                          |
|-----------------------|-----------|------------------------------------------------|
| `aws_region`          | no        | e.g. `us-east-1`                               |
| `environment`         | no        | `stage` or `prod`                              |
| `vpc_id`              | no        | Leave empty to let Terraform create a VPC      |
| `private_subnet_ids`  | no        | Two subnets in different AZs                   |
| `public_subnet_ids`   | no        | Two subnets (needed only when `vpc_id` is set) |
| `container_image`     | no        | Full image URI with digest                     |
| `backplane_url`       | no        |                                                |
| `backplane_client_id` | no        |                                                |
| `internal_fqdn`       | no        | Must match global workspace value              |
| `public_zone_id`      | no        | Must match global workspace value              |

IAM ARNs, S3 bucket name, and Secrets Manager ARN are **not** workspace
variables in the regional workspace — they are read automatically from the
global workspace's remote state.

---

## Local Testing

For testing against an AWS account directly (without HCP Terraform), each root
is applied with the Terraform CLI using a local state backend. The only
complication is that `terraform/regional/remote_state.tf` points to the HCP
Terraform remote backend by default. Terraform's **override file** feature
solves this without touching any source file: a gitignored
`local_override.tf` placed in `terraform/regional/` replaces the remote
backend with a local one that reads from the global state file on disk.

### Step 1 — apply global

```bash
cd terraform/global

terraform init

terraform apply \
  -var="aws_region=us-east-1" \
  -var="environment=stage" \
  -var="s3_bucket_name=rosa-trusted-actions" \
  -var="internal_fqdn=rosa-trusted-actions.internal.company.com" \
  -var="backplane_client_secret=$BACKPLANE_CLIENT_SECRET" \
  -var="ocm_client_secret=$OCM_CLIENT_SECRET" \
  -var="ocm_token=$OCM_TOKEN"
```

Or use a var-file (never commit secrets):

```hcl
# terraform/global/terraform.tfvars  (gitignored)
aws_region     = "us-east-1"
environment    = "stage"
s3_bucket_name = "rosa-trusted-actions"
internal_fqdn  = "rosa-trusted-actions.internal.company.com"
```

```hcl
# $HOME/.config/rosa-ta/secrets.tfvars  (outside the repo)
backplane_client_secret = "..."
ocm_client_secret       = "..."
ocm_token               = "..."
```

```bash
terraform apply \
  -var-file=terraform.tfvars \
  -var-file=$HOME/.config/rosa-ta/secrets.tfvars
```

### Step 2 — create the regional override file

Create this file **once** on your machine. It is gitignored and never
committed. It overrides the `data "terraform_remote_state" "global"` block in
`remote_state.tf` to use the local state file produced by Step 1 instead of
the HCP Terraform remote backend.

```hcl
# terraform/regional/local_override.tf — LOCAL TESTING ONLY. Gitignored.
data "terraform_remote_state" "global" {
  backend = "local"
  config = {
    path = "../global/terraform.tfstate"
  }
}
```

### Step 3 — apply regional

```bash
cd ../regional

terraform init

terraform apply \
  -var="aws_region=us-east-1" \
  -var="environment=stage" \
  -var="vpc_id=vpc-008ef33919b443f10" \
  -var='private_subnet_ids=["subnet-0042826174855e520","subnet-0f9392e3159168d6f"]' \
  -var='public_subnet_ids=["subnet-01821c41d92b0f8b1","subnet-0f52e06526c080dfa"]' \
  -var="container_image=quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:<digest>" \
  -var="backplane_url=https://api.stage.backplane.openshift.com" \
  -var="backplane_client_id=trusted-actions" \
  -var="internal_fqdn=rosa-trusted-actions.internal.company.com"
```

Or with var-files:

```hcl
# terraform/regional/terraform.tfvars  (gitignored)
aws_region          = "us-east-1"
environment         = "stage"
vpc_id              = "vpc-008ef33919b443f10"
private_subnet_ids  = ["subnet-0042826174855e520", "subnet-0f9392e3159168d6f"]
public_subnet_ids   = ["subnet-01821c41d92b0f8b1", "subnet-0f52e06526c080dfa"]
container_image     = "quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:<digest>"
backplane_url       = "https://api.stage.backplane.openshift.com"
backplane_client_id = "trusted-actions"
internal_fqdn       = "rosa-trusted-actions.internal.company.com"
```

```bash
terraform apply -var-file=terraform.tfvars
```

### How the override works

Terraform processes files ending in `_override.tf` after all other files in
the directory and merges their blocks on top. A `data` block with the same
type and name as one in a regular file **completely replaces** it. The source
file `remote_state.tf` is never modified — the override exists only in the
local working copy and is gitignored.

```
remote_state.tf          →  data "terraform_remote_state" "global" { backend = "remote" ... }
local_override.tf        →  data "terraform_remote_state" "global" { backend = "local"  ... }
                                                                      ↑ wins at apply time
```

### Teardown

Destroy regional before global (global owns IAM roles that regional's ECS
tasks depend on at runtime).

```bash
cd terraform/regional && terraform destroy -var-file=terraform.tfvars
cd ../global          && terraform destroy \
  -var-file=terraform.tfvars \
  -var-file=$HOME/.config/rosa-ta/secrets.tfvars
```

---

## Verification

```bash
# From terraform/regional/ after apply
cd terraform/regional

# Tasks running
aws ecs list-tasks \
  --cluster rosa-trusted-actions \
  --region us-east-1 \
  --query 'taskArns'

# Health check — must be run from inside the VPC (bastion, SSM session, or ECS exec)
curl https://$(terraform output -raw internal_fqdn)/health
# {"status":"healthy","version":"..."}

# Container logs
aws logs filter-log-events \
  --log-group-name "$(terraform output -raw cloudwatch_log_group)" \
  --region us-east-1 \
  --filter-pattern "started"
```

---

## Adding a New Region

1. Apply the global workspace once — it is already multi-region aware (IAM
   policies use `arn:aws:*:*` wildcards for region).

2. Create a new HCP Terraform workspace entry in
   `hcp-terraform/rosa-trusted-actions/main.tf` following the
   `rosa-trusted-actions-stage` block. Set `working_directory =
   "terraform/regional"` and provide the region-specific variables.

3. Add the new workspace ID to `tfe_workspace_settings.global_state_sharing`
   so it can read the global remote state.

4. Add a `tfe_workspace_run_trigger` pointing the new workspace at the global
   workspace.

5. Apply the new workspace. `terraform_remote_state` will pick up the global
   outputs automatically.

For local testing of the new region, follow the [Local Testing](#local-testing)
steps with the appropriate `aws_region`, `vpc_id`, and subnet variables for
that region. The same `local_override.tf` file works unchanged — it points to
the shared `terraform/global/terraform.tfstate` regardless of which region is
being tested.
