package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector"
	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/pkg/testutils"
)

type stubTransformer struct {
	prefix string
	err    error
}

func (s stubTransformer) Transform(_ context.Context, in string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.prefix + in, nil
}

var _ collector.Transformer = stubTransformer{}

func TestBuildCanonical_Raw_RunsMinifyAndTransform(t *testing.T) {
	min := stubTransformer{prefix: "M("}
	tfm := stubTransformer{prefix: "T("}

	got, ok := buildCanonical(context.Background(), archiver.PayloadKindRaw, "raw", min, tfm, testutils.Logger())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "T(M(raw" {
		t.Fatalf("expected raw → M → T chain, got %q", got)
	}
}

func TestBuildCanonical_EmptyKind_FallsBackToRaw(t *testing.T) {
	min := stubTransformer{prefix: "M("}
	tfm := stubTransformer{prefix: "T("}

	got, ok := buildCanonical(context.Background(), "", "raw", min, tfm, testutils.Logger())
	if !ok || got != "T(M(raw" {
		t.Fatalf("expected back-compat raw chain, got=%q ok=%v", got, ok)
	}
}

func TestBuildCanonical_Minified_SkipsMinify(t *testing.T) {
	min := stubTransformer{err: errors.New("MUST NOT BE CALLED")}
	tfm := stubTransformer{prefix: "T("}

	got, ok := buildCanonical(context.Background(), archiver.PayloadKindMinified, "min", min, tfm, testutils.Logger())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "T(min" {
		t.Fatalf("expected T-only chain, got %q", got)
	}
}

func TestBuildCanonical_Canonical_NoStages(t *testing.T) {
	min := stubTransformer{err: errors.New("MUST NOT BE CALLED")}
	tfm := stubTransformer{err: errors.New("MUST NOT BE CALLED")}

	got, ok := buildCanonical(context.Background(), archiver.PayloadKindCanonical, "canonical-payload", min, tfm, testutils.Logger())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "canonical-payload" {
		t.Fatalf("expected identity, got %q", got)
	}
}

func TestBuildCanonical_MinifyError_Fails(t *testing.T) {
	min := stubTransformer{err: errors.New("boom-min")}
	tfm := stubTransformer{prefix: "T("}

	_, ok := buildCanonical(context.Background(), archiver.PayloadKindRaw, "raw", min, tfm, testutils.Logger())
	if ok {
		t.Fatal("expected ok=false on minify error")
	}
}

func TestBuildCanonical_TransformError_Fails(t *testing.T) {
	min := stubTransformer{prefix: "M("}
	tfm := stubTransformer{err: errors.New("boom-tfm")}

	_, ok := buildCanonical(context.Background(), archiver.PayloadKindMinified, "min", min, tfm, testutils.Logger())
	if ok {
		t.Fatal("expected ok=false on transform error")
	}
}

func TestBuildCanonical_UnknownKind_TreatsAsCanonical(t *testing.T) {
	min := stubTransformer{err: errors.New("MUST NOT BE CALLED")}
	tfm := stubTransformer{err: errors.New("MUST NOT BE CALLED")}

	got, ok := buildCanonical(context.Background(), archiver.PayloadKind("future-kind"), "x", min, tfm, testutils.Logger())
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(got, "x") {
		t.Fatalf("expected payload preserved, got %q", got)
	}
}
