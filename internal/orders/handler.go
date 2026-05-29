package orders

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

type HandlerFunc func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error)

func LambdaHandler(app *App, initErr error, fn func(*App, context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error)) HandlerFunc {
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		if initErr != nil {
			slog.Error("application initialization failed", "error", initErr)
			return ErrorResponse(http.StatusInternalServerError, "service is not configured")
		}
		return fn(app, ctx, req)
	}
}
