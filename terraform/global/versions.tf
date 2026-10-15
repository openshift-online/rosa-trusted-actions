terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# Global resources are account-scoped, not region-scoped, but the AWS provider
# still requires a region. us-east-1 is used as the canonical home for IAM,
# S3, and Secrets Manager — all of which are globally accessible regardless of
# the provider region setting.
provider "aws" {
  region = var.aws_region
}
