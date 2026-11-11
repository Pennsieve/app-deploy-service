###############################################################################
# Fixture bundles (docs/builds.md "Fixture bundles").                         #
#                                                                             #
# After a release's image is promoted, the status lambda starts a build of   #
# this project, which runs the platform's app-fixtures image as a Dockerfile #
# RUN step (image-builder/fixtures.sh) to materialise the app's review-block #
# inputs, and puts the bundle and a manifest in the fixtures bucket under    #
# fixtures/<applicationId>/<tag>/. The generators and dataset fetches are the #
# app author's code: they run in the same sandbox as an image build, which  #
# is why this project lives in the build VPC and is only enabled with it.   #
# Its role can pull app-fixtures, write its own prefix of the bucket, and    #
# log. Nothing else.                                                          #
###############################################################################

locals {
  fixture_builder_enabled      = local.build_vpc_enabled && var.fixture_builder && var.app_fixtures_image != ""
  fixture_builder_project_name = "${var.environment_name}-${var.service_name}-fixture-builder-${local.region_shortname}"
  fixture_builder_log_group    = "/aws/codebuild/${local.fixture_builder_project_name}"
  fixtures_bucket_name         = "${var.environment_name}-${var.service_name}-fixtures-${local.region_shortname}"

  fixture_builder_project_name_or_empty = local.fixture_builder_enabled ? local.fixture_builder_project_name : ""
  fixtures_bucket_name_or_empty         = local.fixture_builder_enabled ? aws_s3_bucket.fixtures[0].bucket : ""
}

# --- the bucket -------------------------------------------------------------
# Platform-internal: never published, never on Discover. Bundles are
# content-addressed under fixtures/<applicationId>/<tag>/; the manifest.json
# beside them is what the status lambda reads. The lambda tags bundles older
# than an app's previous release retain=no; the lifecycle expires those.
resource "aws_s3_bucket" "fixtures" {
  count = local.fixture_builder_enabled ? 1 : 0

  bucket = local.fixtures_bucket_name
  tags = merge(local.common_tags, {
    "Name"         = local.fixtures_bucket_name
    "service_name" = var.service_name
    "tier"         = "s3"
  })
}

resource "aws_s3_bucket_public_access_block" "fixtures" {
  count = local.fixture_builder_enabled ? 1 : 0

  bucket                  = aws_s3_bucket.fixtures[0].id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "fixtures" {
  count = local.fixture_builder_enabled ? 1 : 0

  bucket = aws_s3_bucket.fixtures[0].id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "fixtures" {
  count = local.fixture_builder_enabled ? 1 : 0

  bucket = aws_s3_bucket.fixtures[0].id

  rule {
    id     = "expire-superseded-bundles"
    status = "Enabled"
    filter {
      tag {
        key   = "retain"
        value = "no"
      }
    }
    expiration {
      days = var.fixture_bundle_expiry_days
    }
  }

  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {
      prefix = ""
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

# --- the project ------------------------------------------------------------
resource "aws_cloudwatch_log_group" "fixture_builder" {
  count = local.fixture_builder_enabled ? 1 : 0

  name              = local.fixture_builder_log_group
  retention_in_days = var.image_builder_log_retention_days
  kms_key_id        = var.image_builder_log_kms_key_arn

  tags = local.common_tags
}

resource "aws_iam_role" "fixture_builder" {
  count = local.fixture_builder_enabled ? 1 : 0

  name = "${var.environment_name}-${var.service_name}-fixture-builder-role-${local.region_shortname}"
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

resource "aws_iam_role_policy" "fixture_builder" {
  count = local.fixture_builder_enabled ? 1 : 0

  name   = "${var.environment_name}-${var.service_name}-fixture-builder-policy-${local.region_shortname}"
  role   = aws_iam_role.fixture_builder[0].id
  policy = data.aws_iam_policy_document.fixture_builder[0].json
}

data "aws_iam_policy_document" "fixture_builder" {
  count = local.fixture_builder_enabled ? 1 : 0

  statement {
    sid       = "ECRLogin"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    # app-fixtures, from the App Store registry (one repository for every
    # app; the image is pinned by app_fixtures_image).
    sid    = "PullAppFixtures"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = [local.appstore_private_ecr_arn]
  }

  statement {
    sid       = "WriteBundles"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.fixtures[0].arn}/fixtures/*"]
  }

  statement {
    sid       = "OwnLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.fixture_builder[0].arn}:*"]
  }

  statement {
    sid    = "BuildVpcInterfaces"
    effect = "Allow"
    actions = [
      "ec2:CreateNetworkInterface",
      "ec2:DescribeDhcpOptions",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DeleteNetworkInterface",
      "ec2:DescribeSubnets",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeVpcs",
    ]
    resources = ["*"]
  }

  statement {
    sid       = "BuildVpcInterfacePermission"
    effect    = "Allow"
    actions   = ["ec2:CreateNetworkInterfacePermission"]
    resources = ["arn:aws:ec2:${data.aws_region.current_region.name}:${data.aws_caller_identity.current.account_id}:network-interface/*"]
    condition {
      test     = "StringEquals"
      variable = "ec2:AuthorizedService"
      values   = ["codebuild.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "ec2:Subnet"
      values   = aws_subnet.build_private[*].arn
    }
  }
}

resource "aws_codebuild_project" "fixture_builder" {
  count = local.fixture_builder_enabled ? 1 : 0

  name           = local.fixture_builder_project_name
  description    = "Builds App Store fixture bundles with app-fixtures under BuildKit (app-deploy-service, docs/builds.md)"
  service_role   = aws_iam_role.fixture_builder[0].arn
  build_timeout  = var.fixture_builder_timeout_minutes
  queued_timeout = 60

  artifacts {
    type = "NO_ARTIFACTS"
  }

  environment {
    type                        = "LINUX_CONTAINER"
    compute_type                = var.fixture_builder_compute_type
    image                       = "aws/codebuild/amazonlinux-x86_64-standard:5.0"
    image_pull_credentials_type = "CODEBUILD"
    # buildkitd's process sandbox and bridge network (as the image builder).
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
      name  = "APP_FIXTURES_IMAGE"
      value = var.app_fixtures_image
    }
    environment_variable {
      name  = "FIXTURES_BUCKET"
      value = aws_s3_bucket.fixtures[0].bucket
    }
    environment_variable {
      name  = "MAX_BUNDLE_BYTES"
      value = tostring(var.fixture_bundle_max_bytes)
    }
    environment_variable {
      name  = "WARN_BUNDLE_BYTES"
      value = tostring(var.fixture_bundle_warn_bytes)
    }
    environment_variable {
      name  = "MAX_DATASET_BYTES"
      value = tostring(var.fixture_dataset_max_bytes)
    }
    environment_variable {
      name  = "GENERATE_TIMEOUT"
      value = tostring(var.fixture_generate_timeout_seconds)
    }
    # Set per build by the status lambda: REPOSITORY, TAG, APPLICATION_ID, PORTS.
  }

  vpc_config {
    vpc_id             = aws_vpc.build[0].id
    subnets            = aws_subnet.build_private[*].id
    security_group_ids = [aws_security_group.image_builder[0].id]
  }

  source {
    type = "NO_SOURCE"
    buildspec = templatefile("${path.module}/image-builder/fixtures-buildspec.yml.tpl", {
      buildkitd_lib   = base64gzip(file("${path.module}/image-builder/buildkitd.sh"))
      fixtures_script = base64gzip(file("${path.module}/image-builder/fixtures.sh"))
    })
  }

  logs_config {
    cloudwatch_logs {
      group_name = aws_cloudwatch_log_group.fixture_builder[0].name
    }
    s3_logs {
      status = "DISABLED"
    }
  }

  tags = local.common_tags
}
