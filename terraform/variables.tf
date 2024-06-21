variable "aws_account" {}

variable "aws_region" {}

variable "environment_name" {}

variable "service_name" {}

variable "vpc_name" {}

variable "domain_name" {}

variable "image_tag" {}

// Fargate Task
variable "container_memory" {
  default = "2048"
}

variable "container_cpu" {
  default = "0"
}

variable "image_url" {
  default = "pennsieve/app-provisioner"
}

variable "task_cpu" {
  default = "512"
}

variable "task_memory" {
  default = "2048"
}

variable "tier" {
  default = "app-provisioner"
}

variable "lambda_bucket" {
  default = "pennsieve-cc-lambda-functions-use1"
}

variable "deployer_image_url" {
  default = "gcr.io/kaniko-project/executor"
}

variable "deployer_image_tag" {
  default = "latest"
}

variable "deployer_tier" {
  default = "app-deployer"
}

variable "deployer_task_cpu" {
  default = "4096"
}

variable "deployer_task_memory" {
  default = "24576"
}

locals {
  common_tags = {
    aws_account      = var.aws_account
    aws_region       = data.aws_region.current_region.name
    environment_name = var.environment_name
  }
}