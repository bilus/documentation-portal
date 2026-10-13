package portal

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Option adjusts how the portal renders the markdown of its docs sections.
// Without options it renders as it always has: raw HTML is dropped and code
// blocks are plain.
type Option func(*renderOptions)

type renderOptions struct {
	video     bool
	highlight bool
	style     string
}

// WithEmbeddedVideo renders the raw HTML of markdown files instead of
// dropping it, and lets the sanitiser keep HTML5 video players: <video> with
// controls, width, height, poster, preload, muted, loop and playsinline, and
// <source> with src and type, where src and poster are https URLs. The
// sanitiser still removes active content, styles and every element and
// attribute it does not know, so the rest of the raw HTML comes through only
// as far as its user-generated-content policy allows.
func WithEmbeddedVideo() Option {
	return func(o *renderOptions) { o.video = true }
}

// DefaultCodeStyle is the chroma style of WithCodeHighlighting when it names
// none.
const DefaultCodeStyle = "github-dark"

// WithCodeHighlighting highlights fenced code blocks by the language of their
// info string, in the colors of the named chroma style, or of
// DefaultCodeStyle when style is empty, and gives every code block of a
// document page that style's background, a block without a language or with
// an unknown one included. Inline code keeps its look. New refuses a style
// chroma does not know.
func WithCodeHighlighting(style string) Option {
	return func(o *renderOptions) {
		o.highlight = true
		o.style = style
		if o.style == "" {
			o.style = DefaultCodeStyle
		}
	}
}

// renderer turns the markdown of a document page into HTML without active
// content, with the stylesheet its code blocks need, if any.
type renderer struct {
	markdown  goldmark.Markdown
	sanitizer *bluemonday.Policy
	codeCSS   template.CSS // empty without highlighting
}

// plainRenderer renders as the portal does without options.
var plainRenderer = mustRenderer(renderOptions{})

func mustRenderer(o renderOptions) *renderer {
	r, err := newRenderer(o)
	if err != nil {
		panic(err)
	}
	return r
}

// codeClassPrefix prefixes the classes of highlighted code, so that the
// stylesheet and the sanitiser's rule touch them alone.
const codeClassPrefix = "portal-"

var (
	httpsURL  = regexp.MustCompile(`^https://[^\s"'<>]+$`)
	preload   = regexp.MustCompile(`^(none|metadata|auto)$`)
	videoType = regexp.MustCompile(`^video/[a-z0-9.+-]+$`)
	codeClass = regexp.MustCompile(`^` + codeClassPrefix + `[a-z0-9]+( ` + codeClassPrefix + `[a-z0-9]+)*$`)
)

func newRenderer(o renderOptions) (*renderer, error) {
	r := &renderer{sanitizer: bluemonday.UGCPolicy()}
	extensions := []goldmark.Extender{extension.GFM}
	var rendererOptions []goldmark.Option
	if o.video {
		rendererOptions = append(rendererOptions, goldmark.WithRendererOptions(html.WithUnsafe()))
		r.sanitizer.AllowElements("video", "source")
		r.sanitizer.AllowAttrs("controls", "muted", "loop", "playsinline").OnElements("video")
		r.sanitizer.AllowAttrs("width", "height").Matching(bluemonday.Integer).OnElements("video")
		r.sanitizer.AllowAttrs("preload").Matching(preload).OnElements("video")
		r.sanitizer.AllowAttrs("poster", "src").Matching(httpsURL).OnElements("video")
		r.sanitizer.AllowAttrs("src").Matching(httpsURL).OnElements("source")
		r.sanitizer.AllowAttrs("type").Matching(videoType).OnElements("source")
	}
	if o.highlight {
		style, ok := styles.Registry[o.style]
		if !ok {
			return nil, fmt.Errorf("unknown code highlighting style %q", o.style)
		}
		formatOptions := []chromahtml.Option{chromahtml.WithClasses(true), chromahtml.ClassPrefix(codeClassPrefix)}
		extensions = append(extensions, highlighting.NewHighlighting(
			highlighting.WithCustomStyle(style),
			highlighting.WithFormatOptions(formatOptions...),
		))
		css, err := codeStylesheet(style, formatOptions)
		if err != nil {
			return nil, err
		}
		r.codeCSS = css
		r.sanitizer.AllowAttrs("class").Matching(codeClass).OnElements("pre", "code", "span")
	}
	r.markdown = goldmark.New(append([]goldmark.Option{
		goldmark.WithExtensions(extensions...),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	}, rendererOptions...)...)
	return r, nil
}

// codeStylesheet returns the stylesheet of highlighted code in style: the
// style's token colors, and its background and text color on every code
// block of a page marked portal-code, so that a block chroma leaves plain
// looks like the others.
func codeStylesheet(style *chroma.Style, formatOptions []chromahtml.Option) (template.CSS, error) {
	var buf bytes.Buffer
	bg := style.Get(chroma.Background)
	fmt.Fprintf(&buf, ".portal-code pre{background-color:%s;color:%s;padding:12px 16px;border-radius:6px;overflow-x:auto}", bg.Background, bg.Colour)
	buf.WriteString(".portal-code pre code{background:none;color:inherit;padding:0;border:0}")
	if err := chromahtml.New(formatOptions...).WriteCSS(&buf, style); err != nil {
		return "", err
	}
	return template.CSS(buf.String()), nil
}
