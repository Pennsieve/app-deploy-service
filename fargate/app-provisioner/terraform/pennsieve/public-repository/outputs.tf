output "app_public_ecr_repository" {
  description = "App Public ECR repository"

  value = aws_ecrpublic_repository.public_repo.repository_url
}