# Render Task Definition JSON
data "template_file" "app_provisioner_ecs_task_definition" {
  template = file("${path.module}/task_definition.json.tpl")

  vars = {
    appstore_private_ecr_url  = data.terraform_remote_state.platform_infrastructure.outputs.appstore_private_ecr_repository_url
    aws_region                = data.aws_region.current_region.name
    aws_region_shortname      = data.terraform_remote_state.region.outputs.aws_region_shortname
    container_cpu             = var.container_cpu
    container_memory          = var.container_memory
    environment_name          = var.environment_name
    docker_hub_credentials    = data.terraform_remote_state.platform_infrastructure.outputs.docker_hub_credentials_arn
    image_tag                 = var.image_tag
    image_url                 = var.image_url
    service_name              = var.service_name
    tier                      = var.tier
  }
}

# Create Fargate Task Definition
resource "aws_ecs_task_definition" "app_provisioner_ecs_task_definition" {
  family                   = "${var.environment_name}-${var.service_name}-${var.tier}-task-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  container_definitions    = data.template_file.app_provisioner_ecs_task_definition.rendered

  cpu                = var.task_cpu
  memory             = var.task_memory
  task_role_arn      = aws_iam_role.app_provisioner_fargate_task_iam_role.arn
  execution_role_arn = aws_iam_role.app_provisioner_fargate_task_iam_role.arn

  depends_on = [data.template_file.app_provisioner_ecs_task_definition]
}

# Render Task Definition JSON - Deployer
data "template_file" "app_deployer_ecs_task_definition" {
  template = file("${path.module}/deployer_task_definition.json.tpl")

  vars = {
    aws_region                = data.aws_region.current_region.name
    aws_region_shortname      = data.terraform_remote_state.region.outputs.aws_region_shortname
    container_cpu             = var.deployer_task_cpu
    container_memory          = var.deployer_task_memory
    environment_name          = var.environment_name
    image_tag                 = var.deployer_image_tag
    image_url                 = var.deployer_image_url
    service_name              = var.service_name
    tier                      = var.deployer_tier
  }
}

# Create Fargate Task Definition
resource "aws_ecs_task_definition" "app_deployer_ecs_task_definition" {
  family                   = "${var.environment_name}-${var.service_name}-${var.deployer_tier}-task-${data.terraform_remote_state.region.outputs.aws_region_shortname}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  container_definitions    = data.template_file.app_deployer_ecs_task_definition.rendered

  cpu                = var.deployer_task_cpu
  memory             = var.deployer_task_memory
  task_role_arn      = aws_iam_role.app_provisioner_fargate_task_iam_role.arn # TODO: update
  execution_role_arn = aws_iam_role.app_provisioner_fargate_task_iam_role.arn # TODO: update

  depends_on = [data.template_file.app_deployer_ecs_task_definition]
}