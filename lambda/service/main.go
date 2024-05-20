package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pennsieve/app-deploy-service/service/handler"
)

func main() {
	lambda.Start(handler.AppDeployServiceHandler)
}
