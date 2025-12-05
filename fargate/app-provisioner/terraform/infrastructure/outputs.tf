output "app_ecr_repository" {
  description = "App ECR repository"

  value = aws_ecr_repository.app.repository_url
}

output "app_id" {
  description = "App Task definition ARN"

  value = var.run_on_gpu ? one(aws_ecs_task_definition.application_gpu).arn : one(aws_ecs_task_definition.application).arn
}

output "app_container_name" {
  description = "App Task definition family"

  value = var.run_on_gpu ? one(aws_ecs_task_definition.application_gpu).family : one(aws_ecs_task_definition.application).family
}

output "run_on_gpu" {
  description = "Whether the application runs on GPU"

  value = var.run_on_gpu
}