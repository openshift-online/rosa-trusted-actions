terraform {
  required_version = ">= 1.15, < 2.0"
  required_providers {
    tfe = {
      source  = "hashicorp/tfe"
      version = "0.81.0"
    }
  }
}

provider "tfe" {
  organization = "hp-platform-engineering"
}

module "rosa_trusted_actions" {
  source  = "app.terraform.io/hp-platform-engineering/workspaces/tfe"
  version = "0.0.15"

  organization      = "hp-platform-engineering"
  project_name      = "rosa-trusted-actions"
  meta_project_name = "meta-rosa"
  notification_url  = var.notification_url

  workspaces = {
    # ── Global workspace ───────────────────────────────────────────────────────
    # Run once per account before any regional workspace.
    # Manages: S3, IAM, Secrets Manager, ACM validation CNAME.
    # Outputs are consumed directly by regional workspaces via terraform_remote_state.
    rosa-trusted-actions-global = {
      terraform_version      = "1.16.0"
      auto_apply             = false
      auto_apply_run_trigger = false
      working_directory      = "terraform/global"
      github_repo_org        = "openshift-online"
      github_repo_name       = "rosa-trusted-actions"
      variable_set_names     = ["rosa-trusted-actions-rosa-boundary-stage-default-aws-dynamic-creds"]
      variables = [
        {
          key         = "aws_region"
          value       = "us-east-1"
          category    = "terraform"
          description = "Provider region for the global workspace."
        },
        {
          key      = "environment"
          value    = "stage"
          category = "terraform"
        },
        {
          key      = "s3_bucket_name"
          value    = "rosa-trusted-actions"
          category = "terraform"
        },
        {
          key         = "internal_fqdn"
          value       = "rosa-trusted-actions.internal.company.com"
          category    = "terraform"
          description = "Used only to obtain the ACM validation CNAME token. Must match all regional workspaces."
        },
        # public_zone_id: add when the public apex zone is available.
        # {
        #   key      = "public_zone_id"
        #   value    = "Z0123456789ABCDEF"
        #   category = "terraform"
        # },
        {
          key       = "backplane_client_secret"
          category  = "terraform"
          sensitive = true
        },
        {
          key       = "ocm_client_secret"
          category  = "terraform"
          sensitive = true
        },
        {
          key       = "ocm_token"
          category  = "terraform"
          sensitive = true
        }
      ]
    }

    # ── Regional workspace: us-east-1 stage ────────────────────────────────────
    # Apply after rosa-trusted-actions-global.
    # IAM ARNs, S3 bucket name, and Secrets Manager ARN are read directly from
    # the global workspace state via terraform_remote_state — no manual copying.
    rosa-trusted-actions-stage = {
      terraform_version      = "1.16.0"
      auto_apply             = false
      auto_apply_run_trigger = false
      working_directory      = "terraform/regional"
      github_repo_org        = "openshift-online"
      github_repo_name       = "rosa-trusted-actions"
      variable_set_names     = ["rosa-trusted-actions-rosa-boundary-stage-default-aws-dynamic-creds"]
      variables = [
        {
          key         = "aws_region"
          value       = "us-east-1"
          category    = "terraform"
          description = "AWS region for this deployment."
        },
        {
          key      = "environment"
          value    = "stage"
          category = "terraform"
        },
        {
          key      = "vpc_id"
          value    = "vpc-008ef33919b443f10"
          category = "terraform"
        },
        {
          key      = "public_subnet_ids"
          value    = ["subnet-01821c41d92b0f8b1", "subnet-0f52e06526c080dfa"]
          category = "terraform"
        },
        {
          key      = "private_subnet_ids"
          value    = ["subnet-0042826174855e520", "subnet-0f9392e3159168d6f"]
          category = "terraform"
        },
        {
          key      = "container_image"
          value    = "quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:4a4ee539446cc92ae6ed2cc30e99998ca5f5dd4e2ab6904e5a85b734088c7815"
          category = "terraform"
        },
        {
          key      = "backplane_url"
          value    = "https://api.stage.backplane.openshift.com"
          category = "terraform"
        },
        {
          key      = "backplane_client_id"
          value    = "trusted-actions"
          category = "terraform"
        },
        {
          key         = "internal_fqdn"
          value       = "rosa-trusted-actions.internal.company.com"
          category    = "terraform"
          description = "Private FQDN — must match the value in the global workspace."
        }
      ]
    }
  }
}

# ── Run trigger ───────────────────────────────────────────────────────────────
# Queues a regional run automatically whenever the global workspace finishes
# successfully. Combined with terraform_remote_state in regional/, this means
# a global apply propagates changes end-to-end without any manual steps.

resource "tfe_workspace_run_trigger" "stage_depends_on_global" {
  workspace_id  = module.rosa_trusted_actions.workspace_ids["rosa-trusted-actions-stage"]
  sourceable_id = module.rosa_trusted_actions.workspace_ids["rosa-trusted-actions-global"]
}

# ── State sharing ─────────────────────────────────────────────────────────────
# Grants the regional workspace read access to the global workspace's state,
# which is required for terraform_remote_state to work in the regional root.

resource "tfe_workspace_settings" "global_state_sharing" {
  workspace_id       = module.rosa_trusted_actions.workspace_ids["rosa-trusted-actions-global"]
  remote_state_consumer_ids = [
    module.rosa_trusted_actions.workspace_ids["rosa-trusted-actions-stage"],
  ]
}
