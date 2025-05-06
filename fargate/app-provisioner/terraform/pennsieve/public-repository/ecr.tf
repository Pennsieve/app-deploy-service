resource "aws_ecrpublic_repository" "public_repo" {
  repository_name = "${var.env}-${var.app_slug}"

  catalog_data {
    description = "Public container repo ${var.source_url}"
  }
}