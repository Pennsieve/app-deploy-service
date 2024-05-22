// CREATE FARGATE TASK CLOUDWATCH LOG GROUP
resource "aws_cloudwatch_log_group" "app_provisioner_fargate_cloudwatch_log_group" {
  name              = "/aws/fargate/${var.environment_name}-${var.service_name}-${var.tier}-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  retention_in_days = 7

  tags = local.common_tags
}

// CREATE FARGATE TASK CLOUDWATCH LOG GROUP - deployer
resource "aws_cloudwatch_log_group" "app_deployer_fargate_cloudwatch_log_group" {
  name              = "/aws/fargate/${var.environment_name}-${var.service_name}-${var.deployer_tier}-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  retention_in_days = 7

  tags = local.common_tags
}
