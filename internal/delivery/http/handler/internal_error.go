package handler

import (
	"context"
	"log/slog"
)

func logInternalError(ctx context.Context, operation string, err error) {
	slog.ErrorContext(ctx, "internal handler error", "operation", operation, "error", err)
}
