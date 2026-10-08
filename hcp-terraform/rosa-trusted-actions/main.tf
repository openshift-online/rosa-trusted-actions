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
    rosa-trusted-actions-stage = {
      terraform_version      = "1.16.0"
      # Putting both to false to not make any destructive changes to the boundary AWS account initially.
      auto_apply             = false
      auto_apply_run_trigger = false
      working_directory      = "terraform"
      github_repo_org        = "openshift-online"
      github_repo_name       = "rosa-trusted-actions"
      variable_set_names     = ["rosa-trusted-actions-rosa-boundary-stage-default-aws-dynamic-creds"]
      variables = [
        {
          key         = "aws_region"
          value       = "us-east-1"
          category    = "terraform"
          description = "AWS region"
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
          description = "Private FQDN for the API. Resolvable only inside the VPC via the Route 53 Private Hosted Zone. Must be a subdomain of a real public domain you control when also setting public_zone_id for TLS."
        },
        {
          key         = "manage_validation_record"
          value       = "true"
          category    = "terraform"
          description = "This workspace owns the shared ACM DNS-01 validation CNAME in the public Route 53 zone. Exactly one regional deployment must be true. All other regions must be false."
        }
      ]
    }
  }
}
