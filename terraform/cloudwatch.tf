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

// CREATE STATUS LAMBDA CLOUDWATCH LOG GROUP
resource "aws_cloudwatch_log_group" "status_lambda_cloudwatch_log_group" {
  name              = "/aws/lambda/${aws_lambda_function.status_lambda.function_name}"
  retention_in_days = 14

  tags = local.common_tags
}

resource "aws_cloudwatch_log_subscription_filter" "status_lambda_datadog_subscription" {
  name            = "${aws_cloudwatch_log_group.status_lambda_cloudwatch_log_group.name}-subscription"
  log_group_name  = aws_cloudwatch_log_group.status_lambda_cloudwatch_log_group.name
  filter_pattern  = ""
  destination_arn = data.terraform_remote_state.region.outputs.datadog_delivery_stream_arn
  role_arn        = data.terraform_remote_state.region.outputs.cw_logs_to_datadog_logs_firehose_role_arn
}

// CREATE STATUS EVENT RULE
resource "aws_cloudwatch_event_rule" "status_cloudwatch_event_rule" {
  name        = "${var.environment_name}-${var.service_name}-status-cloudwatch-event-rule-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  description = "Listens for app deploy task state changes"
  event_pattern = jsonencode({
    "detail" : {
      "group" : ["family:${aws_ecs_task_definition.app_deployer_ecs_task_definition.family}"],
    },
    "detail-type" : ["ECS Task State Change"],
    "source" : ["aws.ecs"]
  })

}

resource "aws_cloudwatch_event_target" "status_cloudwatch_event_target" {
  rule      = aws_cloudwatch_event_rule.status_cloudwatch_event_rule.name
  target_id = "${var.environment_name}-${var.service_name}-status-lambda-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  arn       = aws_lambda_function.status_lambda.arn
}
