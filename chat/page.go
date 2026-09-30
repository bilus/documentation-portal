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

// clientOf names the client of r for the rate limit: its network address.
func clientOf(r *http.Request) (map[string]string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
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
	if title := c.lib.Title(); title != "" {
		heading = "Ask about the " + title + " API"
	}
	nav := []navLink{{Label: "API", URL: c.lib.SpecURL()}}
	if docs := c.lib.DocumentsURL(); docs != "" {
		nav = append(nav, navLink{Label: "Documents", URL: docs})
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

// answerHTML renders an answer's markdown as HTML without active content.
func answerHTML(src string) string {
	doc := answers.Parser().Parse(text.NewReader([]byte(src)))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := n.(*ast.Link); ok && entering {
			link.SetAttributeString("target", "_blank")
		}
		return ast.WalkContinue, nil
	})
	var buf bytes.Buffer
	if err := answers.Renderer().Render(&buf, []byte(src), doc); err != nil {
		return "<p>" + html.EscapeString(src) + "</p>"
	}
	return answerPolicy.Sanitize(buf.String())
}
