package orders

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func JSONResponse(statusCode int, body any) (events.APIGatewayProxyResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return events.APIGatewayProxyResponse{}, fmt.Errorf("marshal response: %w", err)
	}

	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"X-Content-Type-Options":      "nosniff",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(data),
	}, nil
}

func ErrorResponse(statusCode int, message string) (events.APIGatewayProxyResponse, error) {
	return JSONResponse(statusCode, errorBody{
		Error:   http.StatusText(statusCode),
		Message: message,
	})
}

func DecodeJSON(body string, dst any) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("request body is required")
	}

	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}

	return nil
}

func PathValue(req events.APIGatewayProxyRequest, key string) string {
	if value := strings.TrimSpace(req.PathParameters[key]); value != "" {
		return value
	}
	return strings.TrimSpace(req.QueryStringParameters[key])
}
