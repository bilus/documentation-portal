package chat

import (
	"unicode/utf8"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/bilus/documentation-portal/portal"
)

// Size caps on what one tool call returns, in bytes.
const (
	maxDocument = 64 << 10
	maxSpecPart = 32 << 10
	maxMatches  = 25
)

type (
	noArgs      struct{}
	documentArg struct {
		Path string `json:"path" jsonschema:"the path of a markdown file, as list_documents gives it"`
	}
	pointerArg struct {
		Pointer string `json:"pointer" jsonschema:"a JSON pointer into the spec without its leading slash, such as paths/~1pets/get or components/schemas/Pet"`
	}
	queryArg struct {
		Query string `json:"query" jsonschema:"a word or phrase to look for, matched without regard to case"`
	}
)

// tools returns the read-only tools over lib.
func tools(lib *portal.Library) ([]tool.Tool, error) {
	var out []tool.Tool
	add := func(t tool.Tool, err error) error {
		out = append(out, t)
		return err
	}
	err := add(functiontool.New(functiontool.Config{
		Name:        "list_documents",
		Description: "Lists the guides: each one's path, title and page url.",
	}, func(agent.Context, noArgs) (map[string]any, error) {
		docs, err := lib.Documents()
		return map[string]any{"documents": docs}, err
	}))
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "read_document",
			Description: "Returns the text of one guide's page, in markdown, with its links pointed at the portal's pages.",
		}, func(_ agent.Context, a documentArg) (map[string]any, error) {
			text, err := lib.ReadDocument(a.Path)
			if err != nil {
				return nil, err
			}
			text, cut := capped(text, maxDocument)
			return map[string]any{"path": a.Path, "text": text, "truncated": cut}, nil
		}))
	}
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "list_operations",
			Description: "Lists the API's operations: each one's method, path, operationId, summary, pointer and page url.",
		}, func(agent.Context, noArgs) (map[string]any, error) {
			ops, err := lib.Operations()
			return map[string]any{"operations": ops}, err
		}))
	}
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "read_spec",
			Description: "Returns one part of the API reference, the OpenAPI spec, as YAML: an operation, a schema, or any other part named by a JSON pointer. Follow a $ref such as #/components/schemas/Pet by reading components/schemas/Pet.",
		}, func(_ agent.Context, a pointerArg) (map[string]any, error) {
			part, err := lib.SpecPart(a.Pointer)
			if err != nil {
				return nil, err
			}
			part, cut := capped(part, maxSpecPart)
			return map[string]any{"pointer": a.Pointer, "yaml": part, "truncated": cut}, nil
		}))
	}
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "search",
			Description: "Finds a word or phrase in the guides and the API reference: each match's place (a guide's path and line, or a JSON pointer into the spec), text and page url.",
		}, func(_ agent.Context, a queryArg) (map[string]any, error) {
			matches, err := lib.Search(a.Query, maxMatches)
			return map[string]any{"matches": matches}, err
		}))
	}
	return out, err
}

// capped returns s cut to at most n bytes on a rune boundary, and whether it
// was cut.
func capped(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}
