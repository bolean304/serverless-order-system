package orders

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

const (
	defaultTableName = "Orders"
	statusPending    = "PENDING"
	statusPaid       = "PAID"
	statusShipped    = "SHIPPED"
	statusCancelled  = "CANCELLED"
)

type DynamoDBAPI interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

type App struct {
	db        DynamoDBAPI
	tableName string
	logger    *slog.Logger
	now       func() time.Time
}

type CreateOrderRequest struct {
	UserID string `json:"userId"`
	Amount int64  `json:"amount"`
}

type UpdateOrderRequest struct {
	Status string `json:"status"`
}

type Order struct {
	OrderID   string `json:"orderId"`
	UserID    string `json:"userId"`
	Amount    int64  `json:"amount"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func NewApp(ctx context.Context) (*App, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	tableName := strings.TrimSpace(os.Getenv("ORDER_TABLE_NAME"))
	if tableName == "" {
		tableName = defaultTableName
	}

	return &App{
		db:        dynamodb.NewFromConfig(cfg),
		tableName: tableName,
		logger:    slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *App) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	switch req.HTTPMethod {
	case http.MethodPost:
		return a.CreateOrder(ctx, req)
	case http.MethodGet:
		return a.GetOrder(ctx, req)
	case http.MethodPatch, http.MethodPut:
		return a.UpdateOrder(ctx, req)
	default:
		return ErrorResponse(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) CreateOrder(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var input CreateOrderRequest
	if err := DecodeJSON(req.Body, &input); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	input.UserID = strings.TrimSpace(input.UserID)
	if err := validateCreateOrder(input); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	orderID := uuid.NewString()
	createdAt := a.now().Format(time.RFC3339)
	order := Order{
		OrderID:   orderID,
		UserID:    input.UserID,
		Amount:    input.Amount,
		Status:    statusPending,
		CreatedAt: createdAt,
	}

	_, err := a.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(a.tableName),
		Item:                orderItem(order),
		ConditionExpression: aws.String("attribute_not_exists(PK) AND attribute_not_exists(SK)"),
	})
	if err != nil {
		a.logger.Error("create order failed", "error", err, "userId", input.UserID)
		return ErrorResponse(http.StatusInternalServerError, "could not create order")
	}

	return JSONResponse(http.StatusCreated, order)
}

func (a *App) GetOrder(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	userID, orderID := requestKeys(req)
	if err := validateKeys(userID, orderID); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	output, err := a.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.tableName),
		Key:       orderKey(userID, orderID),
	})
	if err != nil {
		a.logger.Error("get order failed", "error", err, "userId", userID, "orderId", orderID)
		return ErrorResponse(http.StatusInternalServerError, "could not get order")
	}
	if len(output.Item) == 0 {
		return ErrorResponse(http.StatusNotFound, "order not found")
	}

	order, err := parseOrder(output.Item)
	if err != nil {
		a.logger.Error("parse order failed", "error", err, "userId", userID, "orderId", orderID)
		return ErrorResponse(http.StatusInternalServerError, "stored order is invalid")
	}

	return JSONResponse(http.StatusOK, order)
}

func (a *App) UpdateOrder(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	userID, orderID := requestKeys(req)
	if err := validateKeys(userID, orderID); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	var input UpdateOrderRequest
	if err := DecodeJSON(req.Body, &input); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if err := validateStatus(status); err != nil {
		return ErrorResponse(http.StatusBadRequest, err.Error())
	}

	updatedAt := a.now().Format(time.RFC3339)
	output, err := a.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(a.tableName),
		Key:       orderKey(userID, orderID),
		ExpressionAttributeNames: map[string]string{
			"#status": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status":    &types.AttributeValueMemberS{Value: status},
			":updatedAt": &types.AttributeValueMemberS{Value: updatedAt},
		},
		UpdateExpression:    aws.String("SET #status = :status, updatedAt = :updatedAt"),
		ConditionExpression: aws.String("attribute_exists(PK) AND attribute_exists(SK)"),
		ReturnValues:        types.ReturnValueAllNew,
	})
	if err != nil {
		var conditionFailed *types.ConditionalCheckFailedException
		if errors.As(err, &conditionFailed) {
			return ErrorResponse(http.StatusNotFound, "order not found")
		}
		a.logger.Error("update order failed", "error", err, "userId", userID, "orderId", orderID)
		return ErrorResponse(http.StatusInternalServerError, "could not update order")
	}

	order, err := parseOrder(output.Attributes)
	if err != nil {
		a.logger.Error("parse updated order failed", "error", err, "userId", userID, "orderId", orderID)
		return ErrorResponse(http.StatusInternalServerError, "stored order is invalid")
	}

	return JSONResponse(http.StatusOK, order)
}

func orderKey(userID, orderID string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
		"SK": &types.AttributeValueMemberS{Value: "ORDER#" + orderID},
	}
}

func orderItem(order Order) map[string]types.AttributeValue {
	item := orderKey(order.UserID, order.OrderID)
	item["orderId"] = &types.AttributeValueMemberS{Value: order.OrderID}
	item["userId"] = &types.AttributeValueMemberS{Value: order.UserID}
	item["amount"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(order.Amount, 10)}
	item["status"] = &types.AttributeValueMemberS{Value: order.Status}
	item["createdAt"] = &types.AttributeValueMemberS{Value: order.CreatedAt}
	return item
}

func parseOrder(item map[string]types.AttributeValue) (Order, error) {
	amount, err := strconv.ParseInt(numberValue(item, "amount"), 10, 64)
	if err != nil {
		return Order{}, fmt.Errorf("parse amount: %w", err)
	}

	return Order{
		OrderID:   stringValue(item, "orderId"),
		UserID:    stringValue(item, "userId"),
		Amount:    amount,
		Status:    stringValue(item, "status"),
		CreatedAt: stringValue(item, "createdAt"),
		UpdatedAt: stringValue(item, "updatedAt"),
	}, nil
}

func stringValue(item map[string]types.AttributeValue, key string) string {
	value, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func numberValue(item map[string]types.AttributeValue, key string) string {
	value, ok := item[key].(*types.AttributeValueMemberN)
	if !ok {
		return ""
	}
	return value.Value
}

func requestKeys(req events.APIGatewayProxyRequest) (string, string) {
	return PathValue(req, "userId"), PathValue(req, "orderId")
}

func validateCreateOrder(input CreateOrderRequest) error {
	if input.UserID == "" {
		return errors.New("userId is required")
	}
	if input.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	return nil
}

func validateKeys(userID, orderID string) error {
	if userID == "" {
		return errors.New("userId is required")
	}
	if orderID == "" {
		return errors.New("orderId is required")
	}
	return nil
}

func validateStatus(status string) error {
	switch status {
	case statusPending, statusPaid, statusShipped, statusCancelled:
		return nil
	default:
		return fmt.Errorf("status must be one of %s, %s, %s, %s", statusPending, statusPaid, statusShipped, statusCancelled)
	}
}
