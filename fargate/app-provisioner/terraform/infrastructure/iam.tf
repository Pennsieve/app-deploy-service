// ## App ##
resource "aws_iam_role" "task_role_for_app" { #
  name               = "task_role_for_${var.app_slug}-${var.env}"
  assume_role_policy = data.aws_iam_policy_document.app_role_assume_role.json
  managed_policy_arns = [aws_iam_policy.app_efs_policy.arn]
}

# TODO: resource should be specific EFS ID
resource "aws_iam_policy" "app_efs_policy" { #
  name = "app_role_efs_policy-${var.app_slug}-${var.env}"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "elasticfilesystem:ClientMount",
          "elasticfilesystem:ClientWrite",
          "elasticfilesystem:ClientRootAccess"
        ]
        Effect   = "Allow"
        Resource = "*"
      },
    ]
  })
}

data "aws_iam_policy_document" "app_role_assume_role" { #
  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}

// ECS Task Execution IAM role
resource "aws_iam_role" "execution_role_for_app" { #
  name               = "execution_role_for_${var.app_slug}-${var.env}"
  assume_role_policy = data.aws_iam_policy_document.app_execution_role_assume_role.json
  managed_policy_arns = [aws_iam_policy.app_execution_role_policy.arn]
}

resource "aws_iam_policy" "app_execution_role_policy" { #
  name = "${var.app_slug}_task_execution_role_policy-${var.env}"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:CreateLogGroup"
        ]
        Effect   = "Allow"
        Resource = "*"
      },
    ]
  })
}

data "aws_iam_policy_document" "app_execution_role_assume_role" { #
  statement {
    effect = "Allow"

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }

    actions = ["sts:AssumeRole"]
  }
}