package dev

import (
	"context"
	"errors"

	"github.com/ChiaYuChang/prism/internal/collector"
)

var ErrInjectedMinifyFailure = errors.New("injected minify failure")

type FailingMinifier struct{}

func (FailingMinifier) Transform(_ context.Context, _ string) (string, error) {
	return "", ErrInjectedMinifyFailure
}

var _ collector.Transformer = FailingMinifier{}
