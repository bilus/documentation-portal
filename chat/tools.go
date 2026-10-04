package chat

import (
	"unicode/utf8"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
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
		Spec    string `json:"spec" jsonschema:"the spec that holds the part, as list_operations or search gives it"`
		Pointer string `json:"pointer" jsonschema:"a JSON pointer into the spec without its leading slash, such as paths/~1pets/get or components/schemas/Pet"`
	}
	queryArg struct {
		Query string `json:"query" jsonschema:"a word or phrase to look for, matched without regard to case"`
	}
)

// tools returns the read-only tools over the library of each answer, as
// libraryIn finds it in the answer's context.
func tools() ([]tool.Tool, error) {
	var out []tool.Tool
	add := func(t tool.Tool, err error) error {
		out = append(out, t)
		return err
	}
	err := add(functiontool.New(functiontool.Config{
		Name:        "list_documents",
		Description: "Lists the guides: each one's path, title and page url.",
	}, func(ctx agent.Context, _ noArgs) (map[string]any, error) {
		lib, err := libraryIn(ctx)
		if err != nil {
			return nil, err
		}
		docs, err := lib.Documents()
		return map[string]any{"documents": docs}, err
	}))
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "read_document",
			Description: "Returns the text of one guide's page, in markdown, with its links pointed at the portal's pages.",
		}, func(ctx agent.Context, a documentArg) (map[string]any, error) {
			lib, err := libraryIn(ctx)
			if err != nil {
				return nil, err
			}
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
			Description: "Lists the operations of every API reference: each one's spec, method, path, operationId, summary, pointer and page url.",
		}, func(ctx agent.Context, _ noArgs) (map[string]any, error) {
			lib, err := libraryIn(ctx)
			if err != nil {
				return nil, err
			}
			ops, err := lib.Operations()
			return map[string]any{"operations": ops}, err
		}))
	}
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "read_spec",
			Description: "Returns one part of an API reference, an OpenAPI spec, as YAML: an operation, a schema, or any other part named by its spec and a JSON pointer. Follow a $ref such as #/components/schemas/Pet by reading components/schemas/Pet of the same spec.",
		}, func(ctx agent.Context, a pointerArg) (map[string]any, error) {
			lib, err := libraryIn(ctx)
			if err != nil {
				return nil, err
			}
			part, err := lib.SpecPart(a.Spec, a.Pointer)
			if err != nil {
				return nil, err
			}
			part, cut := capped(part, maxSpecPart)
			return map[string]any{"spec": a.Spec, "pointer": a.Pointer, "yaml": part, "truncated": cut}, nil
		}))
	}
	if err == nil {
		err = add(functiontool.New(functiontool.Config{
			Name:        "search",
			Description: "Finds a word or phrase in the guides and the API references: each match's place (a guide's path and line, or a spec and a JSON pointer into it), text and page url.",
		}, func(ctx agent.Context, a queryArg) (map[string]any, error) {
			lib, err := libraryIn(ctx)
			if err != nil {
				return nil, err
			}
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
