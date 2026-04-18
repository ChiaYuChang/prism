package parser_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/parser"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/ChiaYuChang/prism/internal/collector/mocks"
)

func TestCompositeParser_Parse(t *testing.T) {
	url := "https://example.com/article"
	data := "<html><body>text</body></html>"

	p1 := mocks.NewMockParser(t)
	p2 := mocks.NewMockParser(t)

	cp, err := parser.NewCompositeParser(testutils.Logger(), p1, p2)
	require.NoError(t, err)

	p1.On("Parse", mock.Anything, url, data).Return(&collector.Article{
		Title: "Title from P1",
	}, nil)

	p2.On("Parse", mock.Anything, url, data).Return(&collector.Article{
		Author: "Author from P2",
	}, nil)

	got, err := cp.Parse(context.Background(), url, data)
	require.NoError(t, err)
	require.Equal(t, "Title from P1", got.Title)
	require.Equal(t, "Author from P2", got.Author)
	require.Equal(t, url, got.URL)
}

func TestCompositeParser_Parse_OneFails(t *testing.T) {
	url := "https://example.com/article"
	data := "<html><body>text</body></html>"

	p1 := mocks.NewMockParser(t)
	p2 := mocks.NewMockParser(t)

	cp, err := parser.NewCompositeParser(testutils.Logger(), p1, p2)
	require.NoError(t, err)

	p1.On("Parse", mock.Anything, url, data).Return(nil, errors.New("p1 error"))

	p2.On("Parse", mock.Anything, url, data).Return(&collector.Article{
		Title: "Title from P2",
	}, nil)

	got, err := cp.Parse(context.Background(), url, data)
	require.NoError(t, err)
	require.Equal(t, "Title from P2", got.Title)
}

func TestCompositeParser_Parse_AllFail(t *testing.T) {
	url := "https://example.com/article"
	data := "<html><body>text</body></html>"

	p1 := mocks.NewMockParser(t)

	cp, err := parser.NewCompositeParser(testutils.Logger(), p1)
	require.NoError(t, err)

	p1.On("Parse", mock.Anything, url, data).Return(nil, errors.New("p1 error"))

	_, err = cp.Parse(context.Background(), url, data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "all parsers failed")
}
