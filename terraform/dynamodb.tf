resource "aws_dynamodb_table" "applications_table" {
  name         = "${var.environment_name}-applications-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "uuid"

  attribute {
    name = "uuid"
    type = "S"
  }

  ttl {
    attribute_name = "TimeToExist"
    enabled        = true
  }

  tags = merge(
    local.common_tags,
    {
      "Name"         = "${var.environment_name}-applications-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "${var.environment_name}-applications-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "service_name" = var.service_name
    },
  )
}

resource "aws_dynamodb_table" "deployments_table" {
  name         = "${var.environment_name}-${var.service_name}-deployments-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "applicationId"
  range_key    = "deploymentId"

  attribute {
    name = "applicationId"
    type = "S"
  }

  attribute {
    name = "deploymentId"
    type = "S"
  }

  ttl {
    attribute_name = "TimeToExist"
    enabled        = true
  }

  tags = merge(
    local.common_tags,
    {
      "Name"         = "${var.environment_name}-${var.service_name}-deployments-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "${var.environment_name}-${var.service_name}-deployments-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "service_name" = var.service_name
    },
  )
}