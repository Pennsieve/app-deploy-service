output "app_ecr_repository" {
  description = "App ECR repository"

  value = aws_ecr_repository.app.repository_url
}

output "app_id" {
  description = "App ECR repository"

  value = aws_ecs_task_definition.application.arn
}