package runner

import (
	"context"
)

type Runner interface {
	Run(context.Context) error
}

type ResultRunner[T any] interface {
	Run(ctx context.Context) (T, error)
}
