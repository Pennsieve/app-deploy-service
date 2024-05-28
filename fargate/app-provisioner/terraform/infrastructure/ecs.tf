// ECS Task definition
resource "aws_ecs_task_definition" "application" {
  family                = "${var.app_slug}-${var.account_id}-${var.env}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 2048
  memory                   = 4096
  task_role_arn      = aws_iam_role.task_role_for_app.arn
  execution_role_arn = aws_iam_role.execution_role_for_app.arn

  container_definitions = jsonencode([
    {
      name      = "${var.app_slug}-${var.account_id}-${var.env}"
      image     = aws_ecr_repository.app.repository_url
      essential = true
      portMappings = [
        {
          containerPort = 8081
          hostPort      = 8081
        }
      ]
      mountPoints = [
        {
          sourceVolume = "${var.app_slug}-storage-${var.account_id}-${var.env}"
          containerPath = "/mnt/efs"
          readOnly = false
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group = "/ecs/${var.app_slug}/${var.account_id}-${var.env}"
          awslogs-region = var.region
          awslogs-stream-prefix = "ecs"
          awslogs-create-group = "true"
        }
      }
    }
  ])

  volume {
    name = "${var.app_slug}-storage-${var.account_id}-${var.env}"

    efs_volume_configuration {
      file_system_id          = var.compute_node_efs_id
      root_directory          = "/"
    }
  }
}