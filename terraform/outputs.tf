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