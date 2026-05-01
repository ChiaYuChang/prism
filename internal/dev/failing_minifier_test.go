package dev_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/dev"
)

func TestFailingMinifier_AlwaysErrors(t *testing.T) {
	m := dev.FailingMinifier{}
	out, err := m.Transform(context.Background(), "<html>x</html>")
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
	if !errors.Is(err, dev.ErrInjectedMinifyFailure) {
		t.Fatalf("expected ErrInjectedMinifyFailure, got %v", err)
	}
}
