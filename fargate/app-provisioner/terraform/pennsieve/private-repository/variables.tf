variable "region" {
  type = string
}

variable "env" {
  type = string
}

variable "aws_account" {
  type        = string
  description = "AWS account name for remote state bucket"
}

variable "vpc_name" {
  type        = string
  description = "VPC name for remote state path"
}
