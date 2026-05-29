package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"

	"order-service/internal/orders"
)

func main() {
	app := orders.NewInMemoryApp()

	mux := http.NewServeMux()
	mux.HandleFunc("/orders", handle(app))
	mux.HandleFunc("/users/", handle(app))

	addr := ":8080"
	log.Printf("local order system running at http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handle(app *orders.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := toAPIGatewayRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := app.Handle(context.Background(), req)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		for key, value := range resp.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write([]byte(resp.Body))
	}
}

func toAPIGatewayRequest(r *http.Request) (events.APIGatewayProxyRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return events.APIGatewayProxyRequest{}, fmt.Errorf("read request body: %w", err)
	}

	pathParams, err := pathParameters(r.URL.Path)
	if err != nil {
		return events.APIGatewayProxyRequest{}, err
	}

	query := make(map[string]string, len(r.URL.Query()))
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}

	return events.APIGatewayProxyRequest{
		HTTPMethod:            r.Method,
		Path:                  r.URL.Path,
		PathParameters:        pathParams,
		QueryStringParameters: query,
		Body:                  string(body),
	}, nil
}

func pathParameters(path string) (map[string]string, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "orders" {
		return map[string]string{}, nil
	}
	if len(parts) == 4 && parts[0] == "users" && parts[2] == "orders" {
		return map[string]string{
			"userId":  parts[1],
			"orderId": parts[3],
		}, nil
	}

	return nil, fmt.Errorf("route not found")
}
