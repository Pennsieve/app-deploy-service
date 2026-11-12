###############################################################################
# Sample runs (docs/builds.md "Sample runs").                                 #
#                                                                             #
# Once a release's fixture bundle is ready, the status lambda starts a build #
# of this project, which runs the promoted image once on the bundle with no  #
# network (image-builder/sample-run.sh, docker run --network none) and puts  #
# the results zip and a manifest in the fixtures bucket under               #
# samples/<applicationId>/<tag>/. The release event then carries them, and   #
# curation-service turns them into the sample document. Needs the fixture   #
# builder (the bundle, the bucket, the build VPC).                            #
###############################################################################

locals {
  sample_runner_enabled      = local.fixture_builder_enabled && var.sample_runner
  sample_runner_project_name = "${var.environment_name}-${var.service_name}-sample-runner-${local.region_shortname}"
  sample_runner_log_group    = "/aws/codebuild/${local.sample_runner_project_name}"

  sample_runner_project_name_or_empty = local.sample_runner_enabled ? local.sample_runner_project_name : ""
}

resource "aws_cloudwatch_log_group" "sample_runner" {
  count = local.sample_runner_enabled ? 1 : 0

  name              = local.sample_runner_log_group
  retention_in_days = var.image_builder_log_retention_days
  kms_key_id        = var.image_builder_log_kms_key_arn

  tags = local.common_tags
}

resource "aws_iam_role" "sample_runner" {
  count = local.sample_runner_enabled ? 1 : 0

  name = "${var.environment_name}-${var.service_name}-sample-runner-role-${local.region_shortname}"
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

resource "aws_iam_role_policy" "sample_runner" {
  count = local.sample_runner_enabled ? 1 : 0

  name   = "${var.environment_name}-${var.service_name}-sample-runner-policy-${local.region_shortname}"
  role   = aws_iam_role.sample_runner[0].id
  policy = data.aws_iam_policy_document.sample_runner[0].json
}

# Pull the promoted image, read the bundle, write the results, log. Nothing
# else; the container itself has no network and no credentials.
data "aws_iam_policy_document" "sample_runner" {
  count = local.sample_runner_enabled ? 1 : 0

  statement {
    sid       = "ECRLogin"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid    = "PullAppStoreImage"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:BatchGetImage",
      "ecr:GetDownloadUrlForLayer",
    ]
    resources = [local.appstore_private_ecr_arn]
  }

  statement {
    sid       = "ReadBundles"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.fixtures[0].arn}/fixtures/*"]
  }

  statement {
    sid       = "WriteResults"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.fixtures[0].arn}/samples/*"]
  }

  statement {
    sid       = "OwnLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.sample_runner[0].arn}:*"]
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

resource "aws_codebuild_project" "sample_runner" {
  count = local.sample_runner_enabled ? 1 : 0

  name           = local.sample_runner_project_name
  description    = "Runs an App Store release once on its fixture bundle, with no network (app-deploy-service, docs/builds.md)"
  service_role   = aws_iam_role.sample_runner[0].arn
  build_timeout  = var.sample_runner_timeout_minutes
  queued_timeout = 60

  artifacts {
    type = "NO_ARTIFACTS"
  }

  environment {
    type                        = "LINUX_CONTAINER"
    compute_type                = var.sample_runner_compute_type
    image                       = "aws/codebuild/amazonlinux-x86_64-standard:5.0"
    image_pull_credentials_type = "CODEBUILD"
    # docker run needs the daemon the standard image starts in privileged mode.
    privileged_mode = true

    environment_variable {
      name  = "FIXTURES_BUCKET"
      value = aws_s3_bucket.fixtures[0].bucket
    }
    environment_variable {
      name  = "RUN_TIMEOUT"
      value = tostring(var.sample_run_timeout_seconds)
    }
    environment_variable {
      name  = "MAX_OUTPUT_BYTES"
      value = tostring(var.sample_output_max_bytes)
    }
    # Set per run by the status lambda: IMAGE, BUNDLE_KEY, APPLICATION_ID,
    # TAG, PARAMETERS, MEMORY_MB; and the compute type for the app's memory.
  }

  vpc_config {
    vpc_id             = aws_vpc.build[0].id
    subnets            = aws_subnet.build_private[*].id
    security_group_ids = [aws_security_group.image_builder[0].id]
  }

  source {
    type = "NO_SOURCE"
    buildspec = templatefile("${path.module}/image-builder/sample-buildspec.yml.tpl", {
      sample_script = base64gzip(file("${path.module}/image-builder/sample-run.sh"))
    })
  }

  logs_config {
    cloudwatch_logs {
      group_name = aws_cloudwatch_log_group.sample_runner[0].name
    }
    s3_logs {
      status = "DISABLED"
    }
  }

  tags = local.common_tags
}

# --- the sample document --------------------------------------------------
# curation-service's App Sample Ready events (once per sample job that
# ended) go to the status lambda, which records sample{...} on the version
# (lambda/status/handler/sample_doc.go).
resource "aws_cloudwatch_event_rule" "sample_ready" {
  count = local.sample_runner_enabled ? 1 : 0

  name        = "${var.environment_name}-${var.service_name}-sample-ready-${local.region_shortname}"
  description = "Sample documents curation-service made for App Store releases"
  event_pattern = jsonencode({
    "source" : ["pennsieve.curation-service"],
    "detail-type" : ["App Sample Ready"],
  })
  tags = local.common_tags
}

resource "aws_cloudwatch_event_target" "sample_ready" {
  count = local.sample_runner_enabled ? 1 : 0

  rule      = aws_cloudwatch_event_rule.sample_ready[0].name
  target_id = "${var.environment_name}-${var.service_name}-sample-ready-${local.region_shortname}"
  arn       = aws_lambda_function.status_lambda.arn
}

resource "aws_lambda_permission" "sample_ready" {
  count = local.sample_runner_enabled ? 1 : 0

  statement_id  = "AllowExecutionFromSampleReadyRule"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.status_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.sample_ready[0].arn
}


# --- a sample run on the owner's compute node -------------------------------
# The service lambda's App Sample Run Registered events (an owner's run on
# their node, its results archive copied under samples/) go to the status
# lambda, which records the sample run from the manifest and emits App
# Release Ready (lambda/status/handler/samples.go, HandleSampleRegistered).
resource "aws_cloudwatch_event_rule" "sample_registered" {
  count = local.fixture_builder_enabled ? 1 : 0

  name        = "${var.environment_name}-${var.service_name}-sample-registered-${local.region_shortname}"
  description = "Sample runs owners made on their compute nodes for App Store releases"
  event_pattern = jsonencode({
    "source" : ["pennsieve.app-deploy-service"],
    "detail-type" : ["App Sample Run Registered"],
  })
  tags = local.common_tags
}

resource "aws_cloudwatch_event_target" "sample_registered" {
  count = local.fixture_builder_enabled ? 1 : 0

  rule      = aws_cloudwatch_event_rule.sample_registered[0].name
  target_id = "${var.environment_name}-${var.service_name}-sample-registered-${local.region_shortname}"
  arn       = aws_lambda_function.status_lambda.arn
}

resource "aws_lambda_permission" "sample_registered" {
  count = local.fixture_builder_enabled ? 1 : 0

  statement_id  = "AllowExecutionFromSampleRegisteredRule"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.status_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.sample_registered[0].arn
}
