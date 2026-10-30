variable "aws_account" {}

variable "aws_region" {}

variable "environment_name" {}

variable "service_name" {}

variable "vpc_name" {}

variable "domain_name" {}

variable "image_tag" {}

// Fargate Task
variable "container_memory" {
  default = "2048"
}

variable "container_cpu" {
  default = "0"
}

variable "image_url" {
  default = "pennsieve/app-provisioner"
}

variable "task_cpu" {
  default = "512"
}

variable "task_memory" {
  default = "2048"
}

variable "tier" {
  default = "app-provisioner"
}

variable "lambda_bucket" {
  default = "pennsieve-cc-lambda-functions-use1"
}

variable "api_domain_name" {
  default = "api2.pennsieve.net"
}

variable "deployer_image_url" {
  default = "gcr.io/kaniko-project/executor"
}

# kaniko v1.24.0, the version ":latest" resolved to (the project is archived,
# so it will not move again; pinned so it cannot change under us). The tag is
# informational; the digest (the multi-arch index) is what the task runs.
variable "deployer_image_tag" {
  default = "v1.24.0"
}

variable "deployer_image_digest" {
  description = "Digest the deployer (kaniko) image is pinned to; empty runs deployer_image_tag."
  default     = "sha256:4e7a52dd1f14872430652bb3b027405b8dfd17c4538751c620ac005741ef9698"
}

variable "deployer_tier" {
  default = "app-deployer"
}

variable "deployer_task_cpu" {
  default = "4096"
}

variable "deployer_task_memory" {
  default = "24576"
}

# Publish the app.yml schema (app-manifest/v1.json) to the platform's schemas
# host (schemas.<domain>). On where the schema's $id points: prod.
variable "publish_schemas" {
  type    = bool
  default = false
}

# warn: register a release whose app.yml doesn't follow the schema and record
# why on the version; enforce: refuse it.
variable "app_manifest_validation" {
  type    = string
  default = "warn"
}

# App variants (docs/app-manifest-variants.md): true builds every variant of
# a release, each its own image (<hash>-<tag>--<id>) and status; false builds
# only the default variant, as before variant builds.
variable "app_variants_enabled" {
  type    = bool
  default = true
}

# App Store image builds (docs/builds.md): "kaniko" runs the kaniko Fargate
# task, "codebuild" runs BuildKit in the CodeBuild project in codebuild.tf.
# The CodeBuild resources exist only when this is "codebuild".
variable "image_builder" {
  type    = string
  default = "kaniko"

  validation {
    condition     = contains(["kaniko", "codebuild"], var.image_builder)
    error_message = "The image_builder value must be \"kaniko\" or \"codebuild\"."
  }
}

# 8 vCPU / 15 GB / 128 GB disk. Fits the CUDA images (3.8-16 GB; dynasd used
# 10.8 GB of BuildKit state). A build whose app.yml needs more than 128 GB
# (APP_STORAGE) runs on BUILD_GENERAL1_XLARGE instead (provisioner).
variable "image_builder_compute_type" {
  type    = string
  default = "BUILD_GENERAL1_LARGE"
}

variable "image_builder_timeout_minutes" {
  type    = number
  default = 60
}

# BuildKit release the builds run, checked against its sha256 (the
# linux-amd64 tarball on github.com/moby/buildkit/releases).
variable "buildkit_version" {
  type    = string
  default = "v0.34.1"
}

variable "buildkit_sha256" {
  type    = string
  default = "a59539daf547fe0958efe2d07e41c0a6944e1112a66d7e519ec1cf5b673ef15b"
}

# Days an app's build cache is kept after its last release.
variable "build_cache_expiry_days" {
  type    = number
  default = 30
}

# Pull base images from Docker Hub with the platform's Docker Hub account
# (platform-infrastructure docker_hub_credentials) instead of anonymously:
# CodeBuild's IP addresses are shared, so anonymous pulls hit Docker Hub's
# per-IP rate limit. Gives the build role read access to that one secret.
variable "image_builder_dockerhub_auth" {
  type    = bool
  default = true
}

# Pull Docker Hub base images through the ECR pull-through cache in this
# account (prefix image_builder_dockerhub_cache_prefix): each image is pulled
# from Docker Hub once, with the platform's account, and builds read the copy
# in ECR. BuildKit falls back to Docker Hub itself when the cache fails. The
# rule is created outside this module (codebuild.tf); turn this on once it
# exists.
variable "image_builder_dockerhub_cache" {
  type    = bool
  default = false
}

variable "image_builder_dockerhub_cache_prefix" {
  type    = string
  default = "docker-hub"
}

variable "image_builder_log_retention_days" {
  type    = number
  default = 14
}

variable "image_builder_log_kms_key_arn" {
  description = "KMS key for the image builder's log group; null for the default encryption."
  type        = string
  default     = null
}

locals {
  common_tags = {
    aws_account      = var.aws_account
    aws_region       = data.aws_region.current_region.name
    environment_name = var.environment_name
  }
}
