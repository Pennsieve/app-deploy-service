output "service_lambda_arn" {
  value = aws_lambda_function.service_lambda.arn
}

output "service_lambda_invoke_arn" {
  value = aws_lambda_function.service_lambda.invoke_arn
}

output "service_lambda_function_name" {
  value = aws_lambda_function.service_lambda.function_name
}

output "applications_table_name" {
  value = aws_dynamodb_table.applications_table.name
}

output "applications_table_arn" {
  value = aws_dynamodb_table.applications_table.arn
}

output "appstore_applications_table_name" {
  value = aws_dynamodb_table.appstore_applications_table.name
}

output "appstore_applications_table_arn" {
  value = aws_dynamodb_table.appstore_applications_table.arn
}

# Read by workflow-service for each version's app.yml (manifest).
output "appstore_versions_table_name" {
  value = aws_dynamodb_table.appstore_versions_table.name
}

output "appstore_versions_table_arn" {
  value = aws_dynamodb_table.appstore_versions_table.arn
}

# Fixture bundles (fixtures.tf): empty when the fixture builder is off.
output "fixtures_bucket_name" {
  value = local.fixtures_bucket_name_or_empty
}

output "fixture_builder_project_name" {
  value = local.fixture_builder_project_name_or_empty
}
