// ECS Task definition (Fargate - CPU only)
resource "aws_ecs_task_definition" "application" {
  count = var.run_on_gpu ? 0 : 1

  family                   = "${var.app_slug}-${var.env}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.app_cpu
  memory                   = var.app_memory
  task_role_arn            = aws_iam_role.task_role_for_app.arn
  execution_role_arn       = aws_iam_role.execution_role_for_app.arn

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
          sourceVolume  = "${var.app_slug}-storage-${var.env}"
          containerPath = "/mnt/efs"
          readOnly      = false
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/${var.app_slug}/${var.env}"
          awslogs-region        = var.region
          awslogs-stream-prefix = "ecs"
          awslogs-create-group  = "true"
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
      file_system_id = var.compute_node_efs_id
      root_directory = "/"
    }
  }

  tags = {
    Environment = var.env
    AppUrl      = var.source_url
  }
}

// ECS Task definition (EC2 - GPU enabled)
resource "aws_ecs_task_definition" "application_gpu" {
  count = var.run_on_gpu ? 1 : 0

  family                   = "${var.app_slug}-${var.env}-gpu"
  requires_compatibilities = ["EC2"]
  network_mode             = "awsvpc"
  cpu                      = var.app_cpu
  memory                   = var.app_memory
  task_role_arn            = aws_iam_role.task_role_for_app.arn
  execution_role_arn       = aws_iam_role.execution_role_for_app.arn

  container_definitions = jsonencode([
    {
      name              = "${var.app_slug}-${var.env}-gpu"
      image             = aws_ecr_repository.app.repository_url
      cpu               = var.app_cpu
      memory            = var.app_memory
      memoryReservation = var.app_memory
      essential         = true
      portMappings = [
        {
          containerPort = 8081
          hostPort      = 8081
          protocol      = "tcp"
        }
      ]
      mountPoints = [
        {
          sourceVolume  = "${var.app_slug}-storage-${var.env}"
          containerPath = "/mnt/efs"
          readOnly      = false
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/${var.app_slug}/${var.env}"
          awslogs-region        = var.region
          awslogs-stream-prefix = "ecs"
          awslogs-create-group  = "true"
        }
      }
      resourceRequirements = [
        {
          type  = "GPU"
          value = "1"
        }
      ]
    }
  ])

  volume {
    name = "${var.app_slug}-storage-${var.env}"

    efs_volume_configuration {
      file_system_id = var.compute_node_efs_id
      root_directory = "/"
    }
  }

  tags = {
    Environment = var.env
    AppUrl      = var.source_url
    RunOnGPU    = "true"
  }
}