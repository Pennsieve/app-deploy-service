output "app_public_ecr_repository" {
  description = "App Public ECR repository"

  value = aws_ecrpublic_repository.public_repo.repository_uri
}

output "app_public_ecr_repository_arn" {
  description = "App Public ECR repository"

  value = aws_ecrpublic_repository.public_repo.arn
}