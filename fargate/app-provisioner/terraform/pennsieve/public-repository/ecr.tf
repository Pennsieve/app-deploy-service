resource "aws_ecrpublic_repository" "public_repo" {
  repository_name = "${var.source_url}"

  catalog_data {
    description = "Public container repo ${var.source_url}"
  }
}