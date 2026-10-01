package portal

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// pageText returns the text of h, the HTML of a document page's body, in
// markdown's notation: the headings, paragraphs, lists, code, tables, links
// and images that a reader sees, and nothing the page leaves out. It also
// returns the text of the page's first level-one heading, or "".
func pageText(h string) (text, title string) {
	doc, err := html.Parse(strings.NewReader(h))
	if err != nil {
		// A strings.Reader never fails, and the parser accepts any input.
		return "", ""
	}
	var w textWriter
	w.children(doc)
	return strings.TrimSpace(w.b.String()), w.title
}

// textWriter writes a page's text, one block after another.
type textWriter struct {
	b     strings.Builder
	need  int    // line breaks owed before the next text
	space bool   // a space owed before the next text on the same line
	lists []list // the open lists, innermost last
	item  bool   // a list item's marker is the last thing written
	title string
}

// list is an open list: ordered or not, and the number of its next item.
type list struct {
	ordered bool
	next    int
}

var spaces = regexp.MustCompile(`\s+`)

// tableParts hold cells and rows, and the line breaks between them are no
// text.
var tableParts = map[atom.Atom]bool{atom.Table: true, atom.Thead: true, atom.Tbody: true, atom.Tfoot: true, atom.Tr: true}

func (w *textWriter) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		w.node(c)
	}
}

func (w *textWriter) node(n *html.Node) {
	if n.Type == html.TextNode {
		if strings.TrimSpace(n.Data) == "" && n.Parent != nil && tableParts[n.Parent.DataAtom] {
			return
		}
		s := spaces.ReplaceAllString(n.Data, " ")
		if rest, ok := strings.CutPrefix(s, " "); ok {
			w.space = true
			s = rest
		}
		rest, trailing := strings.CutSuffix(s, " ")
		w.write(rest)
		w.space = w.space || trailing
		return
	}
	if n.Type != html.ElementNode {
		w.children(n)
		return
	}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		w.block(2)
		w.write(strings.Repeat("#", int(n.Data[1]-'0')) + " ")
		w.children(n)
		if n.DataAtom == atom.H1 && w.title == "" {
			w.title = strings.TrimSpace(spaces.ReplaceAllString(textOf(n), " "))
		}
		w.block(2)
	case atom.P, atom.Blockquote, atom.Table, atom.Div:
		w.block(2)
		w.children(n)
		w.block(2)
	case atom.Ul, atom.Ol:
		l := list{ordered: n.DataAtom == atom.Ol, next: 1}
		if start, err := strconv.Atoi(attr(n, "start")); err == nil {
			l.next = start
		}
		// A list nested in an item starts on the item's next line.
		gap := 2
		if len(w.lists) > 0 {
			gap = 1
		}
		w.block(gap)
		w.lists = append(w.lists, l)
		w.children(n)
		w.lists = w.lists[:len(w.lists)-1]
		w.block(gap)
	case atom.Li:
		marker := "- "
		if depth := len(w.lists); depth > 0 {
			if l := &w.lists[depth-1]; l.ordered {
				marker = strconv.Itoa(l.next) + ". "
				l.next++
			}
			marker = strings.Repeat("  ", depth-1) + marker
		}
		w.block(1)
		w.write(marker)
		w.item = true
		w.children(n)
		w.block(1)
	case atom.Pre:
		w.block(2)
		w.write("```\n" + strings.TrimRight(textOf(n), "\n") + "\n```")
		w.block(2)
	case atom.Code:
		w.write("`" + textOf(n) + "`")
	case atom.A:
		href := attr(n, "href")
		if href == "" {
			w.children(n)
			return
		}
		w.write("[")
		w.children(n)
		w.write("](" + href + ")")
	case atom.Img:
		w.write("![" + attr(n, "alt") + "](" + attr(n, "src") + ")")
	case atom.Br:
		w.block(1)
	case atom.Hr:
		w.block(2)
		w.write("---")
		w.block(2)
	case atom.Tr:
		w.block(1)
		w.write("|")
		w.children(n)
	case atom.Th, atom.Td:
		w.write(" ")
		w.children(n)
		w.write(" |")
	default:
		w.children(n)
	}
}

// block owes n line breaks before the next text, so that the next text
// starts a new line, or a new block after a blank line when n is 2. Right
// after a list item's marker it owes none: the item's first block shares the
// marker's line.
func (w *textWriter) block(n int) {
	if !w.item {
		w.need = max(w.need, n)
	}
}

// write writes s after the line breaks, or else the space, that it owes. A
// space owed at the start of a line, or right after a list item's marker,
// goes unwritten.
func (w *textWriter) write(s string) {
	if s == "" {
		return
	}
	switch {
	case w.b.Len() == 0:
	case w.need > 0:
		w.b.WriteString(strings.Repeat("\n", w.need))
	case w.space && !w.item:
		w.b.WriteByte(' ')
	}
	w.need, w.space, w.item = 0, false, false
	w.b.WriteString(s)
}

// textOf returns the text of n's descendants as it is, for code.
func textOf(n *html.Node) string {
	var b strings.Builder
	var add func(*html.Node)
	add = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			add(c)
		}
	}
	add(n)
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
