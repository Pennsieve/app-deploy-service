package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/app-deploy-service/status/handler"
)

// This Lambda listens for ECS state change events and logs them to DynamoDB

func main() {
	lambda.Start(handler.DeployStateChangeHandler)
}
