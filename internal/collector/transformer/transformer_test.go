package transformer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/stretchr/testify/require"
)

func TestChain_Empty_IsNoOp(t *testing.T) {
	chain := transformer.Chain{}
	out, err := chain.Transform(context.Background(), "hello")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
}

func TestChain_AppliesInOrder(t *testing.T) {
	chain := transformer.Chain{
		transformerFunc(func(_ context.Context, s string) (string, error) {
			return s + "-A", nil
		}),
		transformerFunc(func(_ context.Context, s string) (string, error) {
			return s + "-B", nil
		}),
	}
	out, err := chain.Transform(context.Background(), "start")
	require.NoError(t, err)
	require.Equal(t, "start-A-B", out)
}

func TestChain_StopsOnError(t *testing.T) {
	boom := errors.New("boom")
	chain := transformer.Chain{
		transformerFunc(func(_ context.Context, s string) (string, error) {
			return "", boom
		}),
		transformerFunc(func(_ context.Context, s string) (string, error) {
			t.Fatal("should not be called")
			return s, nil
		}),
	}
	_, err := chain.Transform(context.Background(), "input")
	require.ErrorIs(t, err, boom)
}

type transformerFunc func(context.Context, string) (string, error)

var _ collector.Transformer = (transformerFunc)(nil)

func (f transformerFunc) Transform(ctx context.Context, input string) (string, error) {
	return f(ctx, input)
}
