package transformer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/transformer"
	"github.com/stretchr/testify/require"
)

func TestNoOpTransformer_Identity(t *testing.T) {
	tfm := transformer.NewNoOpTransformer()

	cases := map[string]string{
		"empty":    "",
		"single":   "hello",
		"multi":    "<p>foo</p>\n<p>bar</p>",
		"unicode":  "中文 ASCII 混合",
		"largeish": strings.Repeat("ab", 10_000),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := tfm.Transform(context.Background(), in)
			require.NoError(t, err)
			require.Equal(t, in, out)
		})
	}
}
