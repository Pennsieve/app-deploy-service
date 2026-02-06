[
  {
    "logConfiguration": {
      "logDriver": "awslogs",
      "options": {
        "awslogs-group":"/aws/fargate/${environment_name}-${service_name}-${tier}-${aws_region_shortname}",
        "awslogs-region": "${aws_region}",
        "awslogs-stream-prefix": "fargate"
      }
    },
    "environment": [
      { "name" : "AWS_ACCOUNT", "value": "${aws_account}" },
      { "name" : "ENVIRONMENT", "value": "${environment_name}" },
      { "name" : "ENV", "value": "${environment_name}" },
      { "name" : "REGION", "value": "${aws_region}" },
      { "name" : "VPC_NAME", "value": "${vpc_name}" }
    ],
    "name": "${tier}",
    "image": "${image_url}:${image_tag}",
    "cpu": ${container_cpu},
    "memory": ${container_memory},
    "essential": true,
    "repositoryCredentials": {
      "credentialsParameter": "${docker_hub_credentials}"
    }
  }
]
