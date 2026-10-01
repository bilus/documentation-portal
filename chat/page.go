package chat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/bilus/live-templ/component"
	"github.com/bilus/live-templ/form"
	"github.com/bilus/live-templ/live"
	"github.com/bilus/live-templ/tree"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/bilus/documentation-portal/portal"
)

// Routes returns the chat page at /chat, with the socket and the scripts
// that it needs.
func (c *Chat) Routes() []portal.Route {
	app := live.NewApp()
	var routes []portal.Route
	add := func(pattern string, h http.Handler) {
		routes = append(routes, portal.Route{Pattern: pattern, Handler: h})
	}
	add(app.Handler("/chat", chatComponent, c.mount, live.WithSession(clientOf)))
	add(app.Assets())
	add(app.Socket())
	return routes
}

// clientOf names the client of r for the rate limit: its IPv4 address, or the
// /64 network of its IPv6 address, since one IPv6 host often holds a whole /64.
func clientOf(r *http.Request) (map[string]string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		host = addr.String()
		if network, err := addr.Prefix(64); err == nil && addr.Is6() {
			host = network.String()
		}
	}
	return map[string]string{"client": host}, nil
}

func chatComponent(lv live.Ctx, p *page) live.Component {
	return component.Must(component.New(
		chatPage(lv, p).Render,
		func(context.Context) (*tree.Tree, error) {
			return chatPageTree(lv, p)
		},
	))
}

// page is the state of one chat tab: one conversation.
type page struct {
	chat     *Chat
	client   string
	conv     string
	next     int // the ID of the next message
	Heading  string
	Nav      []navLink
	Messages []message
	Form     questionForm
}

type navLink struct {
	Label, URL string
	Current    bool
}

type message struct {
	ID          int
	Mine, Error bool
	HTML        string
}

// questionForm binds the question textarea to Question.
type questionForm struct {
	form.Form
	Question string `form:"question"`
}

// mount starts a conversation in a new tab.
func (c *Chat) mount(lv live.Ctx) (*page, error) {
	id := make([]byte, 16)
	// crypto/rand.Read is documented never to fail.
	_, _ = rand.Read(id)
	heading := "Ask about the API"
	if names := apis(c.lib.Titles()); names != "" {
		heading = "Ask about " + names
	}
	var nav []navLink
	for _, s := range c.lib.Sections() {
		nav = append(nav, navLink{Label: s.Title, URL: s.URL})
	}
	nav = append(nav, navLink{Label: "Chat", URL: "/chat", Current: true})
	return &page{chat: c, client: lv.Session("client"), conv: hex.EncodeToString(id), Heading: heading, Nav: nav}, nil
}

// FormID changes with every message, so the page renders a new, empty form.
func (p *page) FormID() string { return "ask-" + strconv.Itoa(p.next) }

// Ask answers the question in the form, and adds both to the conversation.
func (p *page) Ask(lv live.Ctx) {
	question := p.Form.Question
	p.add(message{Mine: true, HTML: "<p>" + strings.ReplaceAll(html.EscapeString(strings.TrimSpace(question)), "\n", "<br>") + "</p>"})
	answer, err := p.chat.Ask(lv, p.client, p.conv, question)
	if err != nil {
		p.add(message{Error: true, HTML: "<p>" + html.EscapeString(p.chat.explain(err)) + "</p>"})
		return
	}
	p.add(message{HTML: answerHTML(answer)})
}

func (p *page) add(m message) {
	m.ID = p.next
	p.next++
	p.Messages = append(p.Messages, m)
}

// explain turns an error of Ask into a sentence for the reader.
func (c *Chat) explain(err error) string {
	switch {
	case errors.Is(err, ErrEmpty):
		return "Type a question first."
	case errors.Is(err, ErrTooLong):
		return fmt.Sprintf("Please keep a question under %d characters.", c.limits.QuestionLength)
	case errors.Is(err, ErrRateLimited):
		return "You have asked many questions in a short time. Please try again later."
	case errors.Is(err, ErrTurns):
		return "This conversation has reached its length. Reload the page to start a new one."
	case errors.Is(err, ErrTooManyTools):
		return "I could not find the answer in the documentation in time. Please ask a narrower question."
	case errors.Is(err, ErrDeclined):
		return "I can't help with that. I answer questions about using this API."
	case errors.Is(err, ErrCutOff):
		return "The answer came out too long. Please ask a narrower question."
	}
	log.Printf("chat: %v", err)
	return "The assistant is unavailable right now. Please try again later."
}

var (
	answers = goldmark.New(goldmark.WithExtensions(extension.GFM))
	// The UGC policy, and links that open a new tab, so that following one
	// keeps the conversation.
	answerPolicy = func() *bluemonday.Policy {
		p := bluemonday.UGCPolicy()
		p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")
		return p
	}()
)

// answerHTML renders an answer's markdown as HTML without active content. A
// page of the portal becomes a link that opens in a new tab, so that the
// conversation stays; any other link, and an image from elsewhere, shows as
// text, since the model can write any URL.
func answerHTML(src string) string {
	source := []byte(src)
	doc := answers.Parser().Parse(text.NewReader(source))
	type replacement struct {
		node ast.Node
		tail string // text after the node's children
	}
	var plain []replacement
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			if dest := destination(n.Destination); portalPage(dest) {
				n.SetAttributeString("target", "_blank")
			} else {
				plain = append(plain, replacement{n, " (" + dest + ")"})
			}
		case *ast.Image:
			if !portalPage(destination(n.Destination)) {
				plain = append(plain, replacement{n, ""})
			}
		case *ast.AutoLink:
			plain = append(plain, replacement{n, string(n.Label(source))})
		}
		return ast.WalkContinue, nil
	})
	for _, r := range plain {
		parent := r.node.Parent()
		for c := r.node.FirstChild(); c != nil; c = r.node.FirstChild() {
			parent.InsertBefore(parent, r.node, c)
		}
		if r.tail != "" {
			tail := ast.NewString([]byte(r.tail))
			tail.SetRaw(true)
			parent.InsertBefore(parent, r.node, tail)
		}
		parent.RemoveChild(parent, r.node)
	}
	var buf bytes.Buffer
	if err := answers.Renderer().Render(&buf, source, doc); err != nil {
		return "<p>" + html.EscapeString(src) + "</p>"
	}
	return answerPolicy.Sanitize(buf.String())
}

// destination resolves the backslash escapes and character references of a
// link destination, as goldmark's renderer does before it writes one.
func destination(dest []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(dest))))
}

// portalPage reports whether dest is a path on the portal, such as
// /docs/a.md#setup: it starts with one slash, and has no scheme, no host and
// no backslash, which a browser reads as a slash.
func portalPage(dest string) bool {
	u, err := url.Parse(dest)
	return err == nil && u.Scheme == "" && u.Host == "" &&
		strings.HasPrefix(dest, "/") && !strings.HasPrefix(dest, "//") && !strings.Contains(dest, `\`)
}
