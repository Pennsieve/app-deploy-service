####################################################################
# App Store image builds with BuildKit on CodeBuild (docs/builds.md) #
####################################################################
#
# Exists only when var.image_builder = "codebuild". The provisioner starts one
# build per release (StartBuild); the build runs image-builder/build.sh, which
# builds the release's Dockerfile from git and pushes the image to
# appstore-private with the tag the kaniko task used. CodeBuild's Build State
# Change events go to the status lambda.
#
# Environment-agnostic: names come from environment_name, service_name and
# the region; the appstore-private repository from platform-infrastructure.

locals {
  use_codebuild = var.image_builder == "codebuild"

  region_shortname           = data.terraform_remote_state.region.outputs.aws_region_shortname
  image_builder_project_name = "${var.environment_name}-${var.service_name}-image-builder-${local.region_shortname}"
  image_builder_log_group    = "/aws/codebuild/${local.image_builder_project_name}"

  appstore_private_ecr_arn = data.terraform_remote_state.platform_infrastructure.outputs.appstore_private_ecr_repository_arn

  # A private repo's GitHub token is put here for its build (SecureString,
  # one parameter per deployment, deleted by the status lambda when the
  # build ends), so it is never a plaintext build variable.
  image_build_parameter_prefix = "/${var.environment_name}/${var.service_name}/image-builds"
  image_build_parameter_arn    = "arn:aws:ssm:${data.aws_region.current_region.name}:${data.aws_caller_identity.current.account_id}:parameter${local.image_build_parameter_prefix}/*"

  # empty with kaniko (task_definition.json.tpl)
  image_builder_project_name_or_empty = local.use_codebuild ? local.image_builder_project_name : ""
  build_cache_ecr_url_or_empty        = local.use_codebuild ? aws_ecr_repository.build_cache[0].repository_url : ""

  docker_hub_credentials_arn = data.terraform_remote_state.platform_infrastructure.outputs.docker_hub_credentials_arn

  use_dockerhub_cache = local.use_codebuild && var.image_builder_dockerhub_cache
  # Mirror for docker.io in buildkitd (build.sh): <registry>/<prefix>
  dockerhub_mirror = local.use_dockerhub_cache ? "${data.aws_caller_identity.current.account_id}.dkr.ecr.${data.aws_region.current_region.name}.amazonaws.com/${var.image_builder_dockerhub_cache_prefix}" : ""
}

# --- Docker Hub pull-through cache -------------------------------------------
# The ECR pull-through cache rule for Docker Hub (prefix
# image_builder_dockerhub_cache_prefix) is NOT created here: it needs an
# upstream credential (credential_arn), which AWS provider 4.x doesn't have,
# and the environments run this module on 4.67. It is a registry-wide setting
# anyway; create it where the provider is 5.x (platform-infrastructure), with a
# secret named ecr-pullthroughcache/... holding username and accessToken.
# Until the rule exists, leave image_builder_dockerhub_cache off: with it on,
# buildkitd tries the missing mirror first and falls back to Docker Hub.

# Build cache: one tag per app (provisioner imagebuild.CacheRef), never shared
# between apps. A repository of its own, NOT appstore-private: pushes there
# trigger the SOCI index builder, and cache tags would mix with app tags.
resource "aws_ecr_repository" "build_cache" {
  count = local.use_codebuild ? 1 : 0

  name                 = "${var.environment_name}-appstore-buildcache-${local.region_shortname}"
  image_tag_mutability = "MUTABLE" # each release re-exports its app's cache tag
  force_delete         = true      # only cache; safe to drop when switching back to kaniko

  tags = merge(local.common_tags, {
    "Name"         = "${var.environment_name}-appstore-buildcache-ecr-${local.region_shortname}"
    "service_name" = var.service_name
    "tier"         = "ecr"
  })
}

resource "aws_ecr_lifecycle_policy" "build_cache" {
  count = local.use_codebuild ? 1 : 0

  repository = aws_ecr_repository.build_cache[0].name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Drop superseded cache manifests (a new export untags the old one)"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Drop the cache of an app not released for ${var.build_cache_expiry_days} days"
        selection = {
          tagStatus   = "any"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = var.build_cache_expiry_days
        }
        action = { type = "expire" }
      },
    ]
  })
}

resource "aws_cloudwatch_log_group" "image_builder" {
  count = local.use_codebuild ? 1 : 0

  name              = local.image_builder_log_group
  retention_in_days = var.image_builder_log_retention_days
  kms_key_id        = var.image_builder_log_kms_key_arn

  tags = local.common_tags
}

# Service role: push to appstore-private, read/write the cache, write its own
# logs, read its build's token parameter. Nothing else.
resource "aws_iam_role" "image_builder" {
  count = local.use_codebuild ? 1 : 0

  name = "${var.environment_name}-${var.service_name}-image-builder-role-${local.region_shortname}"
  path = "/service-roles/"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "codebuild.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id }
      }
    }]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "image_builder" {
  count = local.use_codebuild ? 1 : 0

  name   = "${var.environment_name}-${var.service_name}-image-builder-policy-${local.region_shortname}"
  role   = aws_iam_role.image_builder[0].id
  policy = data.aws_iam_policy_document.image_builder[0].json
}

data "aws_iam_policy_document" "image_builder" {
  count = local.use_codebuild ? 1 : 0

  statement {
    sid       = "ECRLogin"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"] # account-wide by definition
    resources = ["*"]
  }

  statement {
    sid    = "PushAppStoreImage"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
      "ecr:PutImage",
      # HEAD on a manifest during the push (does the tag exist yet)
      "ecr:BatchGetImage",
    ]
    resources = [local.appstore_private_ecr_arn]
  }

  statement {
    sid    = "BuildCacheReadWrite"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
      "ecr:PutImage",
    ]
    resources = [aws_ecr_repository.build_cache[0].arn]
  }

  statement {
    sid       = "OwnLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.image_builder[0].arn}:*"]
  }

  statement {
    sid       = "GitTokenParameter"
    effect    = "Allow"
    actions   = ["ssm:GetParameters"]
    resources = [local.image_build_parameter_arn]
  }

  dynamic "statement" {
    for_each = local.use_dockerhub_cache ? [1] : []
    content {
      # A pull through the cache creates the repository and imports the
      # image on a miss.
      sid    = "DockerHubCache"
      effect = "Allow"
      actions = [
        "ecr:BatchCheckLayerAvailability",
        "ecr:BatchGetImage",
        "ecr:GetDownloadUrlForLayer",
        "ecr:BatchImportUpstreamImage",
        "ecr:CreateRepository",
      ]
      resources = ["arn:aws:ecr:${data.aws_region.current_region.name}:${data.aws_caller_identity.current.account_id}:repository/${var.image_builder_dockerhub_cache_prefix}/*"]
    }
  }

  dynamic "statement" {
    for_each = var.image_builder_dockerhub_auth ? [1] : []
    content {
      sid       = "DockerHubCredentials"
      effect    = "Allow"
      actions   = ["secretsmanager:GetSecretValue"]
      resources = [local.docker_hub_credentials_arn]
    }
  }
}

resource "aws_codebuild_project" "image_builder" {
  count = local.use_codebuild ? 1 : 0

  name           = local.image_builder_project_name
  description    = "Builds App Store images with BuildKit (app-deploy-service, docs/builds.md)"
  service_role   = aws_iam_role.image_builder[0].arn
  build_timeout  = var.image_builder_timeout_minutes
  queued_timeout = 60

  artifacts {
    type = "NO_ARTIFACTS"
  }

  environment {
    type         = "LINUX_CONTAINER"
    compute_type = var.image_builder_compute_type
    # AWS-maintained (docker, aws CLI, curl); cached on CodeBuild hosts, so no
    # image pull per build. BuildKit itself comes from the pinned tarball.
    image                       = "aws/codebuild/amazonlinux-x86_64-standard:5.0"
    image_pull_credentials_type = "CODEBUILD"
    # buildkitd needs mounts and namespaces to run with its process sandbox
    # on (RUN steps cannot see the builder or its credentials). The flag
    # stays inside this build's own environment.
    privileged_mode = true

    environment_variable {
      name  = "BUILDKIT_VERSION"
      value = var.buildkit_version
    }
    environment_variable {
      name  = "BUILDKIT_SHA256"
      value = var.buildkit_sha256
    }

    environment_variable {
      name  = "DOCKERHUB_MIRROR"
      value = local.dockerhub_mirror
    }

    dynamic "environment_variable" {
      for_each = var.image_builder_dockerhub_auth ? { DOCKERHUB_USERNAME = "username", DOCKERHUB_PASSWORD = "password" } : {}
      content {
        name  = environment_variable.key
        type  = "SECRETS_MANAGER"
        value = "${local.docker_hub_credentials_arn}:${environment_variable.value}"
      }
    }
  }

  source {
    type = "NO_SOURCE"
    buildspec = templatefile("${path.module}/image-builder/buildspec.yml.tpl", {
      build_script = base64encode(file("${path.module}/image-builder/build.sh"))
    })
  }

  logs_config {
    cloudwatch_logs {
      group_name = aws_cloudwatch_log_group.image_builder[0].name
    }
    s3_logs {
      status = "DISABLED"
    }
  }

  tags = local.common_tags
}

# Build state changes (IN_PROGRESS, SUCCEEDED, FAILED, FAULT, STOPPED,
# TIMED_OUT) to the status lambda, beside the kaniko task's ECS events.
resource "aws_cloudwatch_event_rule" "image_builder_status" {
  count = local.use_codebuild ? 1 : 0

  name        = "${var.environment_name}-${var.service_name}-image-builder-status-${local.region_shortname}"
  description = "App Store image build state changes"
  event_pattern = jsonencode({
    "source" : ["aws.codebuild"],
    "detail-type" : ["CodeBuild Build State Change"],
    "detail" : {
      "project-name" : [aws_codebuild_project.image_builder[0].name]
    }
  })
}

resource "aws_cloudwatch_event_target" "image_builder_status" {
  count = local.use_codebuild ? 1 : 0

  rule      = aws_cloudwatch_event_rule.image_builder_status[0].name
  target_id = "${var.environment_name}-${var.service_name}-image-builder-status-${local.region_shortname}"
  arn       = aws_lambda_function.status_lambda.arn
}

resource "aws_lambda_permission" "image_builder_status" {
  count = local.use_codebuild ? 1 : 0

  statement_id  = "AllowExecutionFromImageBuilderStatusRule"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.status_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.image_builder_status[0].arn
}
