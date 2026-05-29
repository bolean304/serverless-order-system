package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"

	"order-service/internal/orders"
)

var app, initErr = orders.NewApp(context.Background())

func main() {
	lambda.Start(orders.LambdaHandler(app, initErr, (*orders.App).Handle))
}
