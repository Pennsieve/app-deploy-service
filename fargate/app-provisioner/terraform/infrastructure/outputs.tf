output "app_ecr_repository" {
  description = "App ECR repository"

  value = aws_ecr_repository.app.repository_url
}