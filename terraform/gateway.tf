data "terraform_remote_state" "api_gateway" {
  backend = "s3"

  config = {
    bucket  = "${var.aws_account}-terraform-state"
    key     = "aws/${data.aws_region.current_region.name}/${var.vpc_name}/${var.environment_name}/pennsieve-go-api/terraform.tfstate"
    region  = "us-east-1"
    profile = var.aws_account
  }
}

locals {
  cors_allowed_origins = concat(
    [
      "https://app.pennsieve.io",
      "https://app.pennsieve.net",
    ],
    var.environment_name != "prod" ? ["http://localhost:3000"] : []
  )
}

resource "aws_apigatewayv2_api" "app_deploy_service_api" {
  name          = "${var.environment_name}-${var.service_name}-api-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  protocol_type = "HTTP"
  description   = "API for the App Deploy Service"
  cors_configuration {
    allow_origins     = local.cors_allowed_origins
    allow_methods     = ["OPTIONS", "GET", "POST", "PUT", "DELETE"]
    allow_headers     = ["*"]
    allow_credentials = true
    expose_headers    = ["*"]
    max_age           = 300
  }
  body = templatefile("${path.module}/app-deploy-service.yml", {
    authorize_lambda_invoke_uri   = data.terraform_remote_state.api_gateway.outputs.authorizer_lambda_invoke_uri
    gateway_authorizer_role       = data.terraform_remote_state.api_gateway.outputs.authorizer_invocation_role
    app_deploy_service_lambda_arn = aws_lambda_function.service_lambda.arn
  })
}

resource "aws_apigatewayv2_api_mapping" "app_deploy_service_api_map" {
  api_id          = aws_apigatewayv2_api.app_deploy_service_api.id
  domain_name     = var.api_domain_name
  stage           = aws_apigatewayv2_stage.app_deploy_service_gateway_stage.id
  api_mapping_key = "applications"
}

resource "aws_apigatewayv2_stage" "app_deploy_service_gateway_stage" {
  api_id = aws_apigatewayv2_api.app_deploy_service_api.id

  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.app_deploy_service_api_log_group.arn

    format = jsonencode({
      requestId               = "$context.requestId"
      sourceIp                = "$context.identity.sourceIp"
      requestTime             = "$context.requestTime"
      protocol                = "$context.protocol"
      httpMethod              = "$context.httpMethod"
      resourcePath            = "$context.resourcePath"
      routeKey                = "$context.routeKey"
      status                  = "$context.status"
      responseLength          = "$context.responseLength"
      integrationErrorMessage = "$context.integrationErrorMessage"
    })
  }
}

resource "aws_apigatewayv2_integration" "app_deploy_service_integration" {
  api_id             = aws_apigatewayv2_api.app_deploy_service_api.id
  integration_type   = "AWS_PROXY"
  connection_type    = "INTERNET"
  integration_method = "POST"
  integration_uri    = aws_lambda_function.service_lambda.invoke_arn
}

resource "aws_lambda_permission" "app_deploy_service_lambda_permission" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.service_lambda.function_name
  principal     = "apigateway.amazonaws.com"

  source_arn = "${aws_apigatewayv2_api.app_deploy_service_api.execution_arn}/*/*"
}

resource "aws_cloudwatch_log_group" "app_deploy_service_api_log_group" {
  name              = "/aws/apigateway/${var.environment_name}-${var.service_name}-api-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  retention_in_days = 14

  tags = local.common_tags
}
