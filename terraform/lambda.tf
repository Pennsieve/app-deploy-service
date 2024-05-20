resource "aws_lambda_function" "service_lambda" {
  description   = "App Deploy Service"
  function_name = "${var.environment_name}-${var.service_name}-service-lambda-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  architectures = ["arm64"]
  role          = aws_iam_role.service_lambda_role.arn
  timeout       = 300
  memory_size   = 128
  s3_bucket     = var.lambda_bucket
  s3_key        = "${var.service_name}/${var.service_name}-${var.image_tag}.zip"

  vpc_config {
    subnet_ids         = tolist(data.terraform_remote_state.vpc.outputs.private_subnet_ids)
    security_group_ids = [data.terraform_remote_state.platform_infrastructure.outputs.rehydration_service_security_group_id]
  }

  environment {
    variables = {
      ENV              = var.environment_name
      PENNSIEVE_DOMAIN = data.terraform_remote_state.account.outputs.domain_name,
      REGION           = var.aws_region
      TASK_DEF_ARN = aws_ecs_task_definition.provisioner_ecs_task_definition.arn,
      CLUSTER_ARN = data.terraform_remote_state.fargate.outputs.ecs_cluster_arn,
      SUBNET_IDS = join(",", data.terraform_remote_state.vpc.outputs.private_subnet_ids),
      SECURITY_GROUP = data.terraform_remote_state.platform_infrastructure.outputs.rehydration_fargate_security_group_id,
      LOG_LEVEL = "info",
      TASK_DEF_CONTAINER_NAME = var.tier,
      APPLICATIONS_TABLE = aws_dynamodb_table.applications_table.name
    }
  }
}
