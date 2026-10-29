# The app.yml schema, published where its $id says: schemas.<domain>/app-manifest/v1.json,
# the platform's JSON schemas host (platform-infrastructure's schema bucket).
# The service validates against the same file, embedded at build time.
resource "aws_s3_object" "app_manifest_schema" {
  count = var.publish_schemas ? 1 : 0

  bucket        = data.terraform_remote_state.platform_infrastructure.outputs.schemas_bucket_name
  key           = "app-manifest/v1.json"
  source        = "${path.module}/../lambda/service/manifestschema/app-manifest.v1.json"
  etag          = filemd5("${path.module}/../lambda/service/manifestschema/app-manifest.v1.json")
  content_type  = "application/schema+json"
  cache_control = "public, max-age=300"
}
