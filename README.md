# Serverless Order System

A small production-style order service built with Go, AWS Lambda, API Gateway, and DynamoDB.

The service supports:

- Create an order
- Get an order
- Update an order status
- Local development with an in-memory store
- AWS deployment using a Lambda custom runtime
- DynamoDB table provisioning with CloudFormation

## Architecture

```text
Client / Postman / API Gateway
        |
        v
AWS Lambda
        |
        v
DynamoDB Orders table
```

The Lambda receives API Gateway proxy events and routes requests by HTTP method:

```text
POST       /orders                         create order
GET        /users/{userId}/orders/{id}     get order
PATCH/PUT  /users/{userId}/orders/{id}     update order status
```

## Tech Stack

- Go 1.24
- AWS Lambda Go SDK
- AWS SDK for Go v2
- DynamoDB
- API Gateway proxy events
- CloudFormation for DynamoDB table setup

## Project Structure

```text
.
├── cmd/
│   ├── main.go              # AWS Lambda entrypoint
│   └── local/
│       └── main.go          # Local HTTP server for development
├── infra/
│   └── dynamodb-table.yaml  # CloudFormation table definition
├── internal/
│   └── orders/
│       ├── api.go           # JSON responses, request decoding helpers
│       ├── app.go           # Order business logic and DynamoDB operations
│       ├── app_test.go      # Unit tests with fake DynamoDB client
│       ├── handler.go       # Lambda init error wrapper
│       └── memory.go        # In-memory local development store
├── go.mod
├── go.sum
└── README.md
```

## DynamoDB Design

Table name:

```text
Orders
```

Primary key:

```text
Partition key: PK  String
Sort key:      SK  String
```

The service stores order records like this:

```text
PK = USER#user-1
SK = ORDER#05ece73d-05bd-459a-802c-25c6d7cf54ec
```

Example item:

```json
{
  "PK": "USER#user-1",
  "SK": "ORDER#05ece73d-05bd-459a-802c-25c6d7cf54ec",
  "orderId": "05ece73d-05bd-459a-802c-25c6d7cf54ec",
  "userId": "user-1",
  "amount": 499,
  "status": "PENDING",
  "createdAt": "2026-05-29T07:29:23Z"
}
```

The `PK` and `SK` names are intentionally generic. This is a common DynamoDB single-table design style and leaves room for additional item types later.

## Order Status Values

Allowed status values:

```text
PENDING
PAID
SHIPPED
CANCELLED
```

New orders are created with:

```text
PENDING
```

## Environment Variables

The Lambda uses this environment variable:

```text
ORDER_TABLE_NAME=Orders
```

If the variable is not set, the code defaults to:

```text
Orders
```

## Run Locally

The local server uses an in-memory database. It does not require AWS credentials or DynamoDB.

Start the local server:

```powershell
go run ./cmd/local
```

Server URL:

```text
http://localhost:8080
```

### Create Order Locally

```powershell
curl -X POST http://localhost:8080/orders `
  -H "Content-Type: application/json" `
  -d "{\"userId\":\"user-1\",\"amount\":499}"
```

Example response:

```json
{
  "orderId": "generated-order-id",
  "userId": "user-1",
  "amount": 499,
  "status": "PENDING",
  "createdAt": "2026-05-29T07:29:23Z"
}
```

### Get Order Locally

Replace `ORDER_ID` with the order ID returned by create:

```powershell
curl http://localhost:8080/users/user-1/orders/ORDER_ID
```

### Update Order Locally

```powershell
curl -X PATCH http://localhost:8080/users/user-1/orders/ORDER_ID `
  -H "Content-Type: application/json" `
  -d "{\"status\":\"PAID\"}"
```

The local in-memory data is lost when the process stops.

## Run Tests

```powershell
go test ./...
```

The tests use a fake DynamoDB client, so they do not call AWS.

## Create DynamoDB Table

You can create the table manually from AWS Console:

```text
Table name: Orders
Partition key: PK  String
Sort key: SK       String
```

Or deploy the CloudFormation template:

```powershell
aws cloudformation deploy `
  --template-file infra/dynamodb-table.yaml `
  --stack-name order-system-dynamodb `
  --parameter-overrides TableName=Orders `
  --region us-east-1
```

The template enables:

- Pay-per-request billing
- Server-side encryption
- Point-in-time recovery

## Lambda IAM Policy

Attach this permission to the Lambda execution role.

Replace the region/account/table ARN with your actual DynamoDB table ARN:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
        "dynamodb:UpdateItem"
      ],
      "Resource": "arn:aws:dynamodb:us-east-1:123456789012:table/Orders"
    }
  ]
}
```

## Build Lambda Zip

This Lambda uses the AWS custom runtime:

```text
provided.al2023
```

Lambda expects the executable inside the zip to be named:

```text
bootstrap
```

Install the AWS Lambda zip helper:

```powershell
go install github.com/aws/aws-lambda-go/cmd/build-lambda-zip@latest
```

Build for Lambda on x86_64:

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
$env:GOCACHE="D:\Golang-pratice-cdoe\order-system\.gocache"

go build -o bootstrap ./cmd
build-lambda-zip -o function.zip bootstrap
```

Upload `function.zip` to Lambda.

Your zip should contain:

```text
function.zip
└── bootstrap
```

If your Lambda architecture is ARM64, build with:

```powershell
$env:GOARCH="arm64"
```

## Lambda Configuration

Set these Lambda options:

```text
Runtime: provided.al2023
Architecture: x86_64
Handler: bootstrap
Environment variable: ORDER_TABLE_NAME=Orders
```

## Lambda Console Test Events

### Create Order

```json
{
  "httpMethod": "POST",
  "path": "/orders",
  "body": "{\"userId\":\"user-1\",\"amount\":499}"
}
```

Expected response:

```json
{
  "statusCode": 201,
  "body": "{\"orderId\":\"...\",\"userId\":\"user-1\",\"amount\":499,\"status\":\"PENDING\",\"createdAt\":\"...\"}"
}
```

### Get Order

Replace the `orderId` with a real order ID:

```json
{
  "httpMethod": "GET",
  "path": "/users/user-1/orders/05ece73d-05bd-459a-802c-25c6d7cf54ec",
  "pathParameters": {
    "userId": "user-1",
    "orderId": "05ece73d-05bd-459a-802c-25c6d7cf54ec"
  }
}
```

Expected response:

```json
{
  "statusCode": 200,
  "body": "{\"orderId\":\"05ece73d-05bd-459a-802c-25c6d7cf54ec\",\"userId\":\"user-1\",\"amount\":499,\"status\":\"PENDING\",\"createdAt\":\"...\"}"
}
```

### Update Order

```json
{
  "httpMethod": "PATCH",
  "path": "/users/user-1/orders/05ece73d-05bd-459a-802c-25c6d7cf54ec",
  "pathParameters": {
    "userId": "user-1",
    "orderId": "05ece73d-05bd-459a-802c-25c6d7cf54ec"
  },
  "body": "{\"status\":\"PAID\"}"
}
```

Expected response:

```json
{
  "statusCode": 200,
  "body": "{\"orderId\":\"...\",\"userId\":\"user-1\",\"amount\":499,\"status\":\"PAID\",\"createdAt\":\"...\",\"updatedAt\":\"...\"}"
}
```

## API Gateway Routes

Create API Gateway routes and connect them to the Lambda:

```text
POST  /orders
GET   /users/{userId}/orders/{orderId}
PATCH /users/{userId}/orders/{orderId}
PUT   /users/{userId}/orders/{orderId}
```

The Lambda expects API Gateway proxy integration events.

## Error Responses

All responses are JSON.

Example validation error:

```json
{
  "error": "Bad Request",
  "message": "amount must be greater than zero"
}
```

Common status codes:

```text
201 Created              order created
200 OK                   order fetched or updated
400 Bad Request          invalid input
404 Not Found            order does not exist
405 Method Not Allowed   unsupported HTTP method
500 Internal Error       AWS/DynamoDB/config error
```

## Notes

- `cmd/main.go` is the AWS Lambda entrypoint.
- `cmd/local/main.go` is only for local development.
- `bootstrap`, `function.zip`, `.gocache`, and other build artifacts are ignored by Git.
- The current service updates order status but does not physically delete orders.
