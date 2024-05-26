output "app_ecr_repository" {
  description = "App ECR repository"

  value = aws_ecr_repository.app.repository_url
}

output "app_id" {
  description = "App Task definition ARN"

  value = aws_ecs_task_definition.application.arn
}

output "app_container_name" {
  description = "App Task definition family"

  value = aws_ecs_task_definition.application.family
}