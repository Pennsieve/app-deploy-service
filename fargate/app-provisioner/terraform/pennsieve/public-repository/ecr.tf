resource "aws_ecrpublic_repository" "public_repo" {
  repository_name = "${var.app_slug}-${var.env}"

  catalog_data {
    description = "Public container repo ${var.app_slug}-${var.env}"
  }
}