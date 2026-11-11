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

# App Store image builds (docs/builds.md) run BuildKit in the CodeBuild
# project in codebuild.tf; kaniko is retired. Kept as a variable, with
# "codebuild" its only value, so the environments that set it keep working.
variable "image_builder" {
  type    = string
  default = "codebuild"

  validation {
    condition     = var.image_builder == "codebuild"
    error_message = "The image_builder value must be \"codebuild\" (kaniko is retired)."
  }
}

# 8 vCPU / 15 GB / 128 GB disk. Fits the CUDA images (3.8-16 GB; dynasd used
# 10.8 GB of BuildKit state). A build whose app.yml needs more than 128 GB
# (APP_STORAGE) runs on BUILD_GENERAL1_XLARGE instead (provisioner).
variable "image_builder_compute_type" {
  type    = string
  default = "BUILD_GENERAL1_LARGE"
}

# Builds push to a staging repository and the status lambda promotes them
# (codebuild.tf). Turn on after a status lambda with promotion is deployed;
# the builder role then loses write access to appstore-private.
variable "image_builder_staging" {
  type    = bool
  default = false
}

# A build's hard stop. Ours finish well inside 30 minutes; a longer build
# is more likely a runaway RUN step than a bigger image.
variable "image_builder_timeout_minutes" {
  type    = number
  default = 30
}

# Builds in a VPC of their own (build-vpc.tf): private subnets behind a NAT
# gateway, HTTP/HTTPS/DNS egress only, DNS Firewall allow-list, query and
# flow logs. Off: CodeBuild's managed network, open egress.
variable "image_builder_in_vpc" {
  type    = bool
  default = false
}

variable "image_builder_vpc_cidr" {
  type    = string
  default = "10.250.0.0/20"
}

# The domains a build may resolve. Package indexes, code hosts, container
# registries, AWS (ECR, S3, CodeBuild), the fixture dataset hosts of the
# review block, and the model hubs ML apps download weights from. Anything
# else answers NXDOMAIN and is in the query log. Extend per environment.
variable "image_builder_allowed_domains" {
  type = list(string)
  default = [
    # Pennsieve: the API, the App Store's schemas (app.yml $schema), Discover
    # and its assets; public endpoints only, the build VPC has no route to
    # anything internal
    "pennsieve.io", "*.pennsieve.io", "pennsieve.net", "*.pennsieve.net", "pennsieve.org", "*.pennsieve.org",
    "pennsieve.ai", "*.pennsieve.ai",
    # AWS: ECR, S3 (the AWS Open Data buckets included), STS, CodeBuild,
    # CloudWatch; and the Open Data registry site
    "*.amazonaws.com", "registry.opendata.aws", "*.opendata.aws",
    # code hosts
    "github.com", "*.github.com", "*.githubusercontent.com", "gitlab.com", "*.gitlab.com", "bitbucket.org",
    # container registries (Docker Hub is mirrored through ECR; the fallback stays)
    "*.docker.io", "*.docker.com", "ghcr.io", "*.pkg.github.com", "quay.io", "*.quay.io", "public.ecr.aws",
    # Python
    "pypi.org", "*.pypi.org", "files.pythonhosted.org", "*.pythonhosted.org",
    "conda.anaconda.org", "repo.anaconda.com", "*.anaconda.org", "*.anaconda.com", "conda-forge.org",
    "download.pytorch.org", "*.pytorch.org",
    # R
    "cran.r-project.org", "cloud.r-project.org", "*.r-project.org", "packagemanager.posit.co", "*.posit.co",
    "bioconductor.org", "*.bioconductor.org",
    # Node, Go, Rust, Julia
    "registry.npmjs.org", "*.npmjs.org", "proxy.golang.org", "sum.golang.org", "storage.googleapis.com",
    "crates.io", "static.crates.io", "pkg.julialang.org", "*.julialang.org",
    # Linux distributions
    "deb.debian.org", "security.debian.org", "*.debian.org", "archive.ubuntu.com", "security.ubuntu.com", "*.ubuntu.com",
    "dl-cdn.alpinelinux.org", "*.alpinelinux.org", "*.fedoraproject.org", "*.centos.org", "*.rockylinux.org",
    # GPU runtimes
    "developer.download.nvidia.com", "*.nvidia.com",
    # model hubs
    "huggingface.co", "*.huggingface.co", "*.hf.co", "cdn-lfs.huggingface.co",
    # the review block's dataset hosts (fixtures built at release)
    "zenodo.org", "*.zenodo.org", "openneuro.org", "*.openneuro.org", "figshare.com", "*.figshare.com", "osf.io", "*.osf.io",
  ]
}

# BuildKit release the builds run, checked against its sha256 (the
# linux-amd64 tarball on github.com/moby/buildkit/releases).
# The Lambda step runtime every App Store image gets at build
# (image-builder/build.sh): the compute-node-aws-provisioner-v2 release whose
# layer image, <registry>/pennsieve/lambda-step-runtime:<version>, is copied
# into the built stage. Empty: images are built without it, and compute nodes
# refuse to run them on Lambda.
variable "lambda_step_runtime_version" {
  type    = string
  default = ""
}

variable "lambda_step_runtime_registry" {
  type    = string
  default = "932986170183.dkr.ecr.us-east-1.amazonaws.com" # pennsieve-cc
}

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
