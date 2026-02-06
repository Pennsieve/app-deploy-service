provider "aws" {
  region = var.region
}

data "aws_region" "current_region" {}

# Import Platform Infrastructure Data to get the pre-created private ECR repository
data "terraform_remote_state" "platform_infrastructure" {
  backend = "s3"

  config = {
    bucket = "${var.aws_account}-terraform-state"
    key    = "aws/${var.region}/${var.vpc_name}/${var.env}/platform-infrastructure/terraform.tfstate"
    region = "us-east-1"
  }
}
