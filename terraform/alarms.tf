# T1 CloudWatch alarms (EPIC 868m2zvjt; standard sets from
# pennsieve-infra-dashboard/docs/alarm-coverage-plan.md). No alarm_actions
# yet — dashboard/console-visible only.
module "service_alarms" {
  source = "git@github.com:Pennsieve/terraform-modules.git//service-alarms"

  environment_name = var.environment_name
  service_name     = var.service_name

  lambdas = {
    service = {
      function_name   = aws_lambda_function.service_lambda.function_name
      timeout_seconds = aws_lambda_function.service_lambda.timeout
    }
    status = {
      function_name   = aws_lambda_function.status_lambda.function_name
      timeout_seconds = aws_lambda_function.status_lambda.timeout
    }
  }

  dynamodb_tables = {
    app-access = aws_dynamodb_table.app_access_table.name
    applications = aws_dynamodb_table.applications_table.name
    appstore-applications = aws_dynamodb_table.appstore_applications_table.name
    appstore-versions = aws_dynamodb_table.appstore_versions_table.name
    deployments = aws_dynamodb_table.deployments_table.name
  }

}
