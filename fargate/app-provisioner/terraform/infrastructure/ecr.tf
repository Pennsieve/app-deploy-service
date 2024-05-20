resource "aws_ecr_repository" "app" {
  name                 = "${var.app_slug}-${var.account_id}-${var.env}"
  image_tag_mutability = "MUTABLE"
  force_delete = true

  image_scanning_configuration {
    scan_on_push = false
  }
}