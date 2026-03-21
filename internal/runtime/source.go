package runtime

import (
	"context"
	"time"

	"max_bot/internal/maxapi"
)

type UpdateHandler func(context.Context, maxapi.Update) error

type UpdateSource interface {
	Run(context.Context, UpdateHandler) error
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
