package orders

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type fakeDynamoDB struct {
	putInput    *dynamodb.PutItemInput
	getOutput   *dynamodb.GetItemOutput
	updateInput *dynamodb.UpdateItemInput
	updateErr   error
}

func (f *fakeDynamoDB) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.putInput = input
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDynamoDB) GetItem(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if f.getOutput == nil {
		return &dynamodb.GetItemOutput{}, nil
	}
	return f.getOutput, nil
}

func (f *fakeDynamoDB) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updateInput = input
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	item := map[string]types.AttributeValue{
		"orderId":   &types.AttributeValueMemberS{Value: "order-1"},
		"userId":    &types.AttributeValueMemberS{Value: "user-1"},
		"amount":    &types.AttributeValueMemberN{Value: "500"},
		"status":    input.ExpressionAttributeValues[":status"],
		"createdAt": &types.AttributeValueMemberS{Value: "2026-05-29T00:00:00Z"},
		"updatedAt": input.ExpressionAttributeValues[":updatedAt"],
	}
	return &dynamodb.UpdateItemOutput{Attributes: item}, nil
}

func testApp(db DynamoDBAPI) *App {
	return &App{
		db:        db,
		tableName: "Orders",
		logger:    slog.Default(),
		now:       func() time.Time { return time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC) },
	}
}

func TestCreateOrderStoresRequestAmount(t *testing.T) {
	db := &fakeDynamoDB{}
	app := testApp(db)

	resp, err := app.CreateOrder(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"userId":" user-1 ","amount":750}`,
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusCreated, resp.Body)
	}
	if got := aws.ToString(db.putInput.TableName); got != "Orders" {
		t.Fatalf("table = %q, want Orders", got)
	}
	if got := numberValue(db.putInput.Item, "amount"); got != "750" {
		t.Fatalf("amount = %q, want 750", got)
	}
	if got := stringValue(db.putInput.Item, "userId"); got != "user-1" {
		t.Fatalf("userId = %q, want user-1", got)
	}

	var order Order
	if err := json.Unmarshal([]byte(resp.Body), &order); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if order.OrderID == "" {
		t.Fatal("orderId was empty")
	}
	if order.Status != statusPending {
		t.Fatalf("status = %q, want %q", order.Status, statusPending)
	}
}

func TestHandleRoutesByHTTPMethod(t *testing.T) {
	db := &fakeDynamoDB{
		getOutput: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"orderId":   &types.AttributeValueMemberS{Value: "order-1"},
				"userId":    &types.AttributeValueMemberS{Value: "user-1"},
				"amount":    &types.AttributeValueMemberN{Value: "500"},
				"status":    &types.AttributeValueMemberS{Value: statusPending},
				"createdAt": &types.AttributeValueMemberS{Value: "2026-05-29T00:00:00Z"},
			},
		},
	}

	resp, err := testApp(db).Handle(context.Background(), events.APIGatewayProxyRequest{
		HTTPMethod:     http.MethodGet,
		PathParameters: map[string]string{"userId": "user-1", "orderId": "order-1"},
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, resp.Body)
	}
}

func TestCreateOrderRejectsUnknownField(t *testing.T) {
	resp, err := testApp(&fakeDynamoDB{}).CreateOrder(context.Background(), events.APIGatewayProxyRequest{
		Body: `{"userId":"user-1","amount":750,"extra":true}`,
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestGetOrderReturnsNotFound(t *testing.T) {
	resp, err := testApp(&fakeDynamoDB{}).GetOrder(context.Background(), events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"userId": "user-1", "orderId": "missing"},
	})
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestUpdateOrderNormalizesStatus(t *testing.T) {
	db := &fakeDynamoDB{}
	app := testApp(db)

	resp, err := app.UpdateOrder(context.Background(), events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"userId": "user-1", "orderId": "order-1"},
		Body:           `{"status":"paid"}`,
	})
	if err != nil {
		t.Fatalf("UpdateOrder returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, http.StatusOK, resp.Body)
	}
	if got := stringValue(map[string]types.AttributeValue{"status": db.updateInput.ExpressionAttributeValues[":status"]}, "status"); got != statusPaid {
		t.Fatalf("status attribute = %q, want %q", got, statusPaid)
	}
}
