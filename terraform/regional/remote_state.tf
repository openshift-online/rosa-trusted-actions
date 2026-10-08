# Reads outputs from the global workspace directly at plan/apply time.
#
# Requirements:
#   1. The global workspace must have been applied at least once (state must exist).
#   2. State sharing must be enabled: in HCP Terraform, go to the global workspace
#      Settings → Remote state sharing and add each regional workspace as a consumer,
#      or enable organisation-wide sharing.
#
# The `local.global` alias keeps call sites concise and makes it obvious that the
# value originated from global rather than a local variable.

data "terraform_remote_state" "global" {
  backend = "remote"

  config = {
    organization = "hp-platform-engineering"
    workspaces = {
      name = "rosa-trusted-actions-global"
    }
  }
}

locals {
  global = data.terraform_remote_state.global.outputs
}
