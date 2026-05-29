package orders

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type memoryDB struct {
	mu    sync.RWMutex
	items map[string]map[string]types.AttributeValue
}

func NewInMemoryApp() *App {
	return &App{
		db:        &memoryDB{items: make(map[string]map[string]types.AttributeValue)},
		tableName: defaultTableName,
		logger:    slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (db *memoryDB) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	key := itemStorageKey(input.Item)
	if _, exists := db.items[key]; exists {
		return nil, &types.ConditionalCheckFailedException{Message: stringPtr("item already exists")}
	}

	db.items[key] = cloneItem(input.Item)
	return &dynamodb.PutItemOutput{}, nil
}

func (db *memoryDB) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	item, exists := db.items[itemStorageKey(input.Key)]
	if !exists {
		return &dynamodb.GetItemOutput{}, nil
	}

	return &dynamodb.GetItemOutput{Item: cloneItem(item)}, nil
}

func (db *memoryDB) UpdateItem(_ context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	key := itemStorageKey(input.Key)
	item, exists := db.items[key]
	if !exists {
		return nil, &types.ConditionalCheckFailedException{Message: stringPtr("item not found")}
	}

	status, ok := input.ExpressionAttributeValues[":status"]
	if !ok {
		return nil, errors.New("missing status update value")
	}
	updatedAt, ok := input.ExpressionAttributeValues[":updatedAt"]
	if !ok {
		return nil, errors.New("missing updatedAt update value")
	}

	item["status"] = status
	item["updatedAt"] = updatedAt
	db.items[key] = item

	return &dynamodb.UpdateItemOutput{Attributes: cloneItem(item)}, nil
}

func itemStorageKey(item map[string]types.AttributeValue) string {
	return stringValue(item, "PK") + "|" + stringValue(item, "SK")
}

func cloneItem(item map[string]types.AttributeValue) map[string]types.AttributeValue {
	clone := make(map[string]types.AttributeValue, len(item))
	for key, value := range item {
		clone[key] = value
	}
	return clone
}

func stringPtr(value string) *string {
	return &value
}
