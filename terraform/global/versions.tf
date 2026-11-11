terraform {
  required_version = ">= 1.9"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

# Global resources are account-scoped, not region-scoped, but the AWS provider
# still requires a region. us-east-1 is used as the canonical home for IAM,
# S3, and Secrets Manager — all of which are globally accessible regardless of
# the provider region setting.
provider "aws" {
  region = var.aws_region

  # Organization-wide tags applied to every taggable resource; resources'
  # own tags are merged on top. ignored_tag_keys keeps AWS-managed and
  # external automation tags from showing as drift.
  default_tags {
    tags = var.default_tags
  }
  ignore_tags {
    keys = var.ignored_tag_keys
  }
}
