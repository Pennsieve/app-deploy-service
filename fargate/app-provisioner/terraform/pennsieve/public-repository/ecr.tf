resource "aws_ecrpublic_repository" "public_repo" {
  repository_name = "${var.source_url_hash}"

  catalog_data {
    description = "${var.source_url}"
  }
}