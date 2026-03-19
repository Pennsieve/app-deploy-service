resource "aws_s3_bucket" "content_sync_bucket" {
  bucket = "${var.environment_name}-${var.service_name}-content-sync-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "content_sync_bucket_encryption" {
  bucket = aws_s3_bucket.content_sync_bucket.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "aws:kms"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "content_sync_bucket_public_access" {
  bucket = aws_s3_bucket.content_sync_bucket.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
