terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

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
