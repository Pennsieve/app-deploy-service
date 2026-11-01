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

# The app domains table (what an app is for, from its tags; the colours and icon
# its banner is drawn in), at its $id: schemas.<domain>/app-domains/v1.json. The
# web app's Apps tab and github-service's Discover banners read the same table.
resource "aws_s3_object" "app_domains" {
  count = var.publish_schemas ? 1 : 0

  bucket        = data.terraform_remote_state.platform_infrastructure.outputs.schemas_bucket_name
  key           = "app-domains/v1.json"
  source        = "${path.module}/../lambda/service/appdomains/app-domains.v1.json"
  etag          = filemd5("${path.module}/../lambda/service/appdomains/app-domains.v1.json")
  content_type  = "application/json"
  cache_control = "public, max-age=300"
}
