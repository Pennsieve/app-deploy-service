// ECS Task definition
resource "aws_ecs_task_definition" "application" {
  family                = "${var.app_slug}-${var.env}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.app_cpu
  memory                   = var.app_memory
  task_role_arn      = aws_iam_role.task_role_for_app.arn
  execution_role_arn = aws_iam_role.execution_role_for_app.arn

  container_definitions = jsonencode([
    {
      name      = "${var.app_slug}-${var.env}"
      image     = aws_ecr_repository.app.repository_url
      cpu       = var.app_cpu
      memory    = var.app_memory
      essential = true
      portMappings = [
        {
          containerPort = 8081
          hostPort      = 8081
        }
      ]
      mountPoints = [
        {
          sourceVolume = "${var.app_slug}-storage-${var.env}"
          containerPath = "/mnt/efs"
          readOnly = false
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group = "/ecs/${var.app_slug}/${var.env}"
          awslogs-region = var.region
          awslogs-stream-prefix = "ecs"
          awslogs-create-group = "true"
        }
      }
    }
  ])

  ephemeral_storage {
    size_in_gib = 30
  }

  volume {
    name = "${var.app_slug}-storage-${var.env}"

    efs_volume_configuration {
      file_system_id          = var.compute_node_efs_id
      root_directory          = "/"
    }
  }

  tags = {
    Environment = "${var.env}"
    AppUrl      = "${var.source_url}"
  }
}