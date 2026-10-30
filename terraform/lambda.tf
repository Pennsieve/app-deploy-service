resource "aws_lambda_function" "service_lambda" {
  # Stamp the deployed build in the description so it's visible at the top of the
  # Lambda Console page. AWS discards the uploaded zip's filename post-deploy, so
  # image_tag is otherwise unrecoverable from the deployed function.
  description   = "App Deploy Service (image_tag: ${var.image_tag})"
  function_name = "${var.environment_name}-${var.service_name}-service-lambda-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.service_lambda_role.arn
  timeout       = 300
  # Lambda's CPU scales with memory: at 128 MB (~1/14 vCPU) the API, the app
  # list most of all, spent ~0.8 s on CPU. 1024 MB is ~8x the CPU at about
  # the same cost per request, since billing is memory x duration.
  memory_size = 1024
  s3_bucket   = var.lambda_bucket
  s3_key      = "${var.service_name}/${var.service_name}-${var.image_tag}.zip"

  # Resource tag: discoverable in the Console (Tags tab) and fleet-wide via the
  # Resource Groups Tagging API (get-resources --tag-filters Key=ImageTag).
  tags = merge(local.common_tags, {
    ImageTag = var.image_tag
  })

  vpc_config {
    subnet_ids = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [
      data.terraform_remote_state.platform_infrastructure.outputs.rehydration_service_security_group_id
    ]
  }

  environment {
    variables = {
      # Deployed build: readable by the running code (os.Getenv("IMAGE_TAG")) and via
      # `aws lambda get-function-configuration ... Environment.Variables.IMAGE_TAG`.
      IMAGE_TAG                        = var.image_tag
      ENV                              = var.environment_name
      PENNSIEVE_DOMAIN                 = data.terraform_remote_state.account.outputs.domain_name,
      REGION                           = var.aws_region
      TASK_DEF_ARN                     = aws_ecs_task_definition.app_provisioner_ecs_task_definition.arn,
      DEPLOYER_TASK_DEF_ARN            = aws_ecs_task_definition.app_deployer_ecs_task_definition.arn,
      CLUSTER_ARN                      = data.terraform_remote_state.fargate.outputs.ecs_cluster_arn,
      SUBNET_IDS                       = join(",", data.terraform_remote_state.vpc.outputs.private_subnet_ids),
      SECURITY_GROUP                   = data.terraform_remote_state.platform_infrastructure.outputs.rehydration_fargate_security_group_id,
      LOG_LEVEL                        = "info",
      TASK_DEF_CONTAINER_NAME          = var.tier,
      DEPLOYER_TASK_DEF_CONTAINER_NAME = var.deployer_tier,
      APPLICATIONS_TABLE               = aws_dynamodb_table.applications_table.name,
      APPSTORE_APPLICATIONS_TABLE      = aws_dynamodb_table.appstore_applications_table.name,
      APPSTORE_VERSIONS_TABLE          = aws_dynamodb_table.appstore_versions_table.name,
      DEPLOYMENTS_TABLE                = aws_dynamodb_table.deployments_table.name,
      APP_ACCESS_TABLE                 = aws_dynamodb_table.app_access_table.name,
      ACCOUNTS_TABLE                   = data.terraform_remote_state.account_service.outputs.accounts_table_name
      CONTENT_SYNC_BUCKET              = aws_s3_bucket.content_sync_bucket.id
      // token_auth routes verify the request's organization_id through the
      // platform's direct authorizer (handler/workspace_access.go). Not
      // wrapped in try(): a missing output must fail the plan, not every
      // workspace check.
      DIRECT_AUTHORIZER_LAMBDA_NAME = data.terraform_remote_state.api_gateway.outputs.direct_authorizer_lambda_name
      // How a release whose app.yml doesn't follow its schema is treated:
      // warn (record the problems on the version) or enforce (refuse it).
      APP_MANIFEST_VALIDATION = var.app_manifest_validation
      // Live updates: the Event API's publish host where realtime is on
      // (realtime.tf); empty keeps Pusher.
      REALTIME_EVENTS_ENDPOINT = local.realtime_events_endpoint
      // Which builder builds App Store images (docs/builds.md); with kaniko,
      // releases whose Dockerfile uses BuildKit-only syntax are refused.
      IMAGE_BUILDER = var.image_builder
      // Build every variant of a release (true), or only the default.
      APP_VARIANTS_ENABLED = tostring(var.app_variants_enabled)
    }
  }
}

resource "aws_lambda_function" "status_lambda" {
  # Stamp the deployed build in the description so it's visible at the top of the
  # Lambda Console page. AWS discards the uploaded zip's filename post-deploy, so
  # image_tag is otherwise unrecoverable from the deployed function.
  description   = "App Deploy Status Listener (image_tag: ${var.image_tag})"
  function_name = "${var.environment_name}-${var.service_name}-status-lambda-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.status_lambda_role.arn
  timeout       = 300
  memory_size   = 128
  s3_bucket     = var.lambda_bucket
  s3_key        = "${var.service_name}/${var.service_name}-status-${var.image_tag}.zip"

  # Resource tag: discoverable in the Console (Tags tab) and fleet-wide via the
  # Resource Groups Tagging API (get-resources --tag-filters Key=ImageTag).
  tags = merge(local.common_tags, {
    ImageTag = var.image_tag
  })

  vpc_config {
    subnet_ids = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [
      data.terraform_remote_state.platform_infrastructure.outputs.rehydration_service_security_group_id
    ]
  }

  environment {
    variables = {
      # Deployed build: readable by the running code (os.Getenv("IMAGE_TAG")) and via
      # `aws lambda get-function-configuration ... Environment.Variables.IMAGE_TAG`.
      IMAGE_TAG                   = var.image_tag
      ENV                         = var.environment_name,
      REGION                      = var.aws_region,
      LOG_LEVEL                   = "info",
      APPLICATIONS_TABLE          = aws_dynamodb_table.applications_table.name,
      APPSTORE_APPLICATIONS_TABLE = aws_dynamodb_table.appstore_applications_table.name,
      DEPLOYMENTS_TABLE           = aws_dynamodb_table.deployments_table.name
    }
  }
}

resource "aws_lambda_permission" "status_rule_permission" {
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.status_lambda.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.status_cloudwatch_event_rule.arn
}
