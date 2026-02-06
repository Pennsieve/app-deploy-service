output "app_private_ecr_repository" {
  description = "App Private ECR repository URL from platform-infrastructure"

  value = data.terraform_remote_state.platform_infrastructure.outputs.appstore_private_ecr_repository_url
}

output "app_private_ecr_repository_arn" {
  description = "App Private ECR repository ARN from platform-infrastructure"

  value = data.terraform_remote_state.platform_infrastructure.outputs.appstore_private_ecr_repository_arn
}
