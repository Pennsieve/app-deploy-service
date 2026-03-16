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

resource "aws_dynamodb_table" "appstore_applications_table" {
  name         = "${var.environment_name}-appstore-applications-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "uuid"

  attribute {
    name = "uuid"
    type = "S"
  }

  attribute {
    name = "sourceUrl"
    type = "S"
  }

  global_secondary_index {
    name            = "sourceUrl-index"
    hash_key        = "sourceUrl"
    projection_type = "ALL"
  }

  tags = merge(
    local.common_tags,
    {
      "Name"         = "${var.environment_name}-appstore-applications-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "${var.environment_name}-appstore-applications-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "service_name" = var.service_name
    },
  )
}

resource "aws_dynamodb_table" "appstore_versions_table" {
  name         = "${var.environment_name}-appstore-versions-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "uuid"

  attribute {
    name = "uuid"
    type = "S"
  }

  attribute {
    name = "applicationId"
    type = "S"
  }

  attribute {
    name = "version"
    type = "S"
  }

  global_secondary_index {
    name            = "applicationId-version-index"
    hash_key        = "applicationId"
    range_key       = "version"
    projection_type = "ALL"
  }

  tags = merge(
    local.common_tags,
    {
      "Name"         = "${var.environment_name}-appstore-versions-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "${var.environment_name}-appstore-versions-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "service_name" = var.service_name
    },
  )
}

resource "aws_dynamodb_table" "app_access_table" {
  name         = "${var.environment_name}-app-access-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "entityId"
  range_key    = "appId"

  attribute {
    name = "entityId"
    type = "S"
  }

  attribute {
    name = "appId"
    type = "S"
  }

  global_secondary_index {
    name            = "appId-entityId-index"
    hash_key        = "appId"
    range_key       = "entityId"
    projection_type = "ALL"
  }

  tags = merge(
    local.common_tags,
    {
      "Name"         = "${var.environment_name}-app-access-table-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
      "name"         = "${var.environment_name}-app-access-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
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