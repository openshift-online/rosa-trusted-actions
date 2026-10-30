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
  version = "0.0.23"

  organization      = "hp-platform-engineering"
  project_name      = "rosa-trusted-actions"
  meta_project_name = "meta-rosa"
  notification_url  = var.notification_url

  notification = {
    triggers = [
      "run:needs_attention",
      "run:errored",
      "assessment:check_failure",
      "assessment:drifted",
      "assessment:failed",
    ]
  }

  workspaces = {
    # ── Global workspace ───────────────────────────────────────────────────────
    # Run once per account before any regional workspace.
    # Manages: IAM, Secrets Manager, ACM validation CNAME.
    # Outputs are consumed directly by regional workspaces via terraform_remote_state.
    rosa-trusted-actions-global = {
      terraform_version      = "1.16.0"
      auto_apply             = false
      auto_apply_run_trigger = false
      working_directory      = "terraform/global"
      github_repo_org        = "openshift-online"
      github_repo_name       = "rosa-trusted-actions"
      variable_set_names     = ["rosa-trusted-actions-rosa-boundary-stage-rosa-trusted-actions-aws-dynamic-creds"]
      # Lets the regional workspace read this workspace's outputs via
      # terraform_remote_state — required for the regional root's
      # terraform_remote_state block to resolve ARNs/bucket names.
      remote_state_consumer_workspaces = ["rosa-trusted-actions-stage"]
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
          key      = "s3_bucket_name_suffix"
          value    = "rosa-trusted-actions"
          category = "terraform"
        },
        {
          key         = "internal_fqdn"
          value       = "rosa-trusted-actions.internal.company.com"
          category    = "terraform"
          description = "Used only to obtain the ACM validation CNAME token. Must match all regional workspaces."
        }
        # public_zone_id: add when the public apex zone is available.
        # {
        #   key      = "public_zone_id"
        #   value    = "Z0123456789ABCDEF"
        #   category = "terraform"
        # },
      ]
    }

    # ── Regional workspace: us-east-1 stage ────────────────────────────────────
    # Apply after rosa-trusted-actions-global.
    # IAM ARNs and the Secrets Manager ARN are read directly from the global
    # workspace state via terraform_remote_state — no manual copying. The S3
    # bucket is created in this workspace (s3_bucket_name_suffix must match
    # the global workspace's value).
    rosa-trusted-actions-stage = {
      terraform_version      = "1.16.0"
      auto_apply             = false
      auto_apply_run_trigger = false
      working_directory      = "terraform/regional"
      github_repo_org        = "openshift-online"
      github_repo_name       = "rosa-trusted-actions"
      # Queues a regional run whenever the global workspace applies
      # successfully (module-managed run trigger, resolved by name since
      # it's a sibling workspace in this same module invocation).
      source_workspaces  = ["rosa-trusted-actions-global"]
      variable_set_names = ["rosa-trusted-actions-rosa-boundary-stage-rosa-trusted-actions-aws-dynamic-creds"]
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
          key         = "s3_bucket_name_suffix"
          value       = "rosa-trusted-actions"
          category    = "terraform"
          description = "Must match the global workspace's s3_bucket_name_suffix — used to build this region's bucket name."
        },
        {
          key      = "vpc_id"
          value    = "vpc-008ef33919b443f10"
          category = "terraform"
        },
        {
          key      = "public_subnet_ids"
          value    = jsonencode(["subnet-01821c41d92b0f8b1", "subnet-0f52e06526c080dfa"])
          category = "terraform"
          hcl      = true
        },
        {
          key      = "private_subnet_ids"
          value    = jsonencode(["subnet-0042826174855e520", "subnet-0f9392e3159168d6f"])
          category = "terraform"
          hcl      = true
        },
        {
          key      = "container_image"
          value    = "quay.io/redhat-user-workloads/rosa-tenant/rosa-trusted-actions@sha256:6c32506aee10340de766407d9ef51211f1a5fd9c87f36d58d2120bf4653430bd"
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
        },
        {
          key       = "ocm_client_id"
          value     = var.ocm_client_id
          category  = "terraform"
          sensitive = true
        },
        {
          key       = "ocm_client_secret"
          value     = var.ocm_client_secret
          category  = "terraform"
          sensitive = true
        },
        {
          key       = "backplane_client_secret"
          value     = var.backplane_client_secret
          category  = "terraform"
          sensitive = true
        }
      ]
    }
  }
}

# ── State sharing ─────────────────────────────────────────────────────────────
# Grants the regional workspace read access to the global workspace's state
# via the module's own remote_state_consumer_workspaces field (see the global
# workspace definition above) rather than a second tfe_workspace_settings
# resource here, which would fight the module's tfe_workspace_settings.this
# for the same workspace and strand the setting on the next unrelated update.
