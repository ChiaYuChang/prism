package minifier_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/minifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustMinify(t *testing.T, raw string) string {
	t.Helper()
	out, err := minifier.New().Transform(context.Background(), raw)
	require.NoError(t, err)
	return out
}

// TestMinify_StripsChromeTags covers the "remove boilerplate tag" half of the
// minifier's job. Each subcase wraps a chrome tag and a content paragraph in a
// body; the chrome-tag content must be gone, the paragraph must survive.
func TestMinify_StripsChromeTags(t *testing.T) {
	cases := []struct {
		name   string
		chrome string
	}{
		{"script", `<script>alert("boom")</script>`},
		{"script_with_src", `<script src="https://evil.example/x.js"></script>`},
		{"script_non_ldjson_type", `<script type="text/javascript">alert("boom")</script>`},
		{"style", `<style>body{color:red}</style>`},
		{"noscript", `<noscript>please enable js</noscript>`},
		{"nav", `<nav>site menu here</nav>`},
		{"header", `<header>site header banner</header>`},
		{"footer", `<footer>site footer boilerplate</footer>`},
		{"aside", `<aside>sidebar widget content</aside>`},
		{"iframe", `<iframe src="https://ads.example"></iframe>`},
		{"form", `<form><input name="email"/></form>`},
		{"role_navigation", `<div role="navigation">skip link</div>`},
		{"role_banner", `<div role="banner">banner text</div>`},
		{"role_complementary", `<div role="complementary">related links</div>`},
		{"class_ad", `<div class="ad">buy this</div>`},
		{"class_ads", `<div class="ads">buy that</div>`},
		{"class_advertisement", `<div class="advertisement">sponsor</div>`},
		{"class_sidebar", `<div class="sidebar">promo</div>`},
		{"class_related", `<div class="related">related items</div>`},
	}

	// Marker phrases chosen to make assertions unambiguous: none of these
	// tokens can appear as residual text of a correctly-stripped element.
	markers := []string{"boom", "color:red", "enable js", "menu here", "header banner",
		"footer boilerplate", "sidebar widget", "ads.example", "email", "skip link",
		"banner text", "related links", "buy this", "buy that", "sponsor", "promo",
		"related items", "evil.example"}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := `<html><body><p>KEEPME</p>` + c.chrome + `</body></html>`
			out := mustMinify(t, in)

			assert.Contains(t, out, "KEEPME", "article paragraph must survive")
			for _, m := range markers {
				assert.NotContains(t, out, m, "chrome marker %q leaked through", m)
			}
		})
	}
}

// TestMinify_PreservesJSONLDScript pins the JSON-LD carve-out: the downstream
// jsonld parser runs AFTER minify, so script[type="application/ld+json"] must
// survive. Regression here would silently blind JSON-LD parsing for every site.
func TestMinify_PreservesJSONLDScript(t *testing.T) {
	const ldJSON = `{"@context":"https://schema.org","@type":"NewsArticle","headline":"Test"}`
	in := `<html><body>` +
		`<script type="application/ld+json">` + ldJSON + `</script>` +
		`<script>alert("strip me")</script>` +
		`<p>body text</p>` +
		`</body></html>`

	out := mustMinify(t, in)

	assert.Contains(t, out, "application/ld+json", "ld+json script tag must be preserved")
	assert.Contains(t, out, `"@type":"NewsArticle"`, "ld+json payload must be preserved")
	assert.Contains(t, out, "body text", "article body must survive")
	assert.NotContains(t, out, "strip me", "non-ld+json script must still be stripped")
}

// TestMinify_PreservesArticleStructure confirms semantic article markup
// (headings, paragraphs, lists, blockquote) survives unchanged.
func TestMinify_PreservesArticleStructure(t *testing.T) {
	in := `<html><body>
		<h1>Main Title</h1>
		<h2>Subtitle</h2>
		<p>First paragraph.</p>
		<p>Second paragraph.</p>
		<ul><li>item one</li><li>item two</li></ul>
		<blockquote>quoted text</blockquote>
	</body></html>`

	out := mustMinify(t, in)

	for _, want := range []string{
		"<h1>", "Main Title",
		"<h2>", "Subtitle",
		"First paragraph.", "Second paragraph.",
		"<ul>", "item one", "item two",
		"<blockquote>", "quoted text",
	} {
		assert.Contains(t, out, want, "article markup %q should be preserved", want)
	}
}

// TestMinify_RemovesEmptyWrappers confirms the second-pass empty-element sweep
// works for div/span/section/article that contribute no text.
func TestMinify_RemovesEmptyWrappers(t *testing.T) {
	in := `<html><body>
		<div></div>
		<span></span>
		<section></section>
		<article></article>
		<p>keep me</p>
	</body></html>`

	out := mustMinify(t, in)

	assert.Contains(t, out, "keep me")
	assert.NotContains(t, out, "<div>")
	assert.NotContains(t, out, "<span>")
	assert.NotContains(t, out, "<section>")
	assert.NotContains(t, out, "<article>")
}

// TestMinify_CascadesEmptyAfterStrip verifies that when a wrapper contains
// ONLY stripped chrome, the now-empty wrapper is also removed (because
// goquery's Text() is recursive, the wrapper appears empty after the strip).
func TestMinify_CascadesEmptyAfterStrip(t *testing.T) {
	in := `<html><body>
		<div><script>alert("x")</script></div>
		<section><nav>menu</nav></section>
		<p>survivor</p>
	</body></html>`

	out := mustMinify(t, in)

	assert.Contains(t, out, "survivor")
	assert.NotContains(t, out, "alert")
	assert.NotContains(t, out, "menu")
	// The wrapping div/section should also be gone; if they remained, we'd see
	// empty "<div></div>" literals in the output stream.
	assert.NotContains(t, out, "<div>")
	assert.NotContains(t, out, "<section>")
}

// TestMinify_CollapsesBlankLines confirms the post-render line trim: every
// output line is non-empty and TrimSpace'd.
func TestMinify_CollapsesBlankLines(t *testing.T) {
	in := "<html><body>\n\n\n   <p>hello</p>\n\n\n   <p>world</p>\n\n\n</body></html>"

	out := mustMinify(t, in)

	for line := range strings.SplitSeq(out, "\n") {
		if line == "" {
			continue // trailing newline after last WriteByte is fine
		}
		assert.Equal(t, strings.TrimSpace(line), line, "line %q should be trimmed", line)
	}
	// No run of two consecutive newlines (which would imply a blank line).
	assert.NotContains(t, out, "\n\n")
}

// TestMinify_EmptyAndWhitespaceInput confirms graceful handling of degenerate
// inputs. goquery wraps naked input in html/body automatically.
func TestMinify_EmptyAndWhitespaceInput(t *testing.T) {
	cases := []string{"", "   ", "\n\n\n", "\t\t"}
	for _, in := range cases {
		t.Run("len"+itoa(len(in)), func(t *testing.T) {
			out, err := minifier.New().Transform(context.Background(), in)
			require.NoError(t, err)
			assert.Equal(t, "", strings.TrimSpace(out), "whitespace-only input should minify to empty")
		})
	}
}

// TestMinify_MalformedHTML confirms goquery's HTML5 parser tolerates unclosed
// and mis-nested tags rather than returning an error.
func TestMinify_MalformedHTML(t *testing.T) {
	in := `<html><body><p>unclosed paragraph<div>nested wrong</p></div><p>next`

	out, err := minifier.New().Transform(context.Background(), in)
	require.NoError(t, err)

	assert.Contains(t, out, "unclosed paragraph")
	assert.Contains(t, out, "nested wrong")
	assert.Contains(t, out, "next")
}

// TestMinify_ImageOnlyDivRemoved pins a known limitation: <div><img/></div> is
// treated as an "empty wrapper" because goquery's Text() returns "" for <img>,
// so the surrounding div (and the image) are stripped. Not currently a
// problem because parser rules target <p> for text and <img>s inside <p>
// caption wrappers survive. Pinned here so any behaviour change is deliberate.
func TestMinify_ImageOnlyDivRemoved(t *testing.T) {
	in := `<html><body>
		<div><img src="x.jpg" alt="caption"/></div>
		<p>caption text</p>
	</body></html>`

	out := mustMinify(t, in)

	assert.Contains(t, out, "caption text")
	assert.NotContains(t, out, "x.jpg",
		"current limitation: image-only wrappers are removed; update test when behaviour changes")
}

// itoa is a tiny helper so subtest names stay readable without importing strconv
// in a test file that has no other need for it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
