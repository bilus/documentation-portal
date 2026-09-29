package portal

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// markerKey marks a part of a spec as unpublished for the targets in its
// value, and as markerKey-<name> it marks the sibling <name>.
const markerKey = "x-doNotPublish"

// target is the publication target of the portal.
const target = "main"

// maxCopies is the most nodes that alias expansion may copy for one spec.
const maxCopies = 100_000

// publishedSpec returns raw without its unpublished parts and marker keys, or
// raw itself when the rules remove nothing.
func publishedSpec(raw []byte) ([]byte, error) {
	var docs []*yaml.Node
	removed := false
	budget := maxCopies
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		doc := new(yaml.Node)
		err := dec.Decode(doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := expandAliases(doc, &budget); err != nil {
			return nil, err
		}
		if unpublish(doc) {
			removed = true
		}
		docs = append(docs, doc)
	}
	if !removed {
		return raw, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// expandAliases replaces every alias below n with a copy of its anchor's
// node, so that the rules see each use of a shared node. It copies at most
// budget nodes: nested aliases can grow a small file exponentially, and an
// alias inside its own anchor grows it without end.
func expandAliases(n *yaml.Node, budget *int) error {
	for i, c := range n.Content {
		if c.Kind == yaml.AliasNode {
			if c = copyNode(c.Alias, budget); c == nil {
				return errors.New("document contains excessive aliasing")
			}
			n.Content[i] = c
		}
		if err := expandAliases(c, budget); err != nil {
			return err
		}
	}
	n.Anchor = ""
	return nil
}

// copyNode returns a deep copy of n and subtracts its node count from budget,
// or nil once budget reaches zero.
func copyNode(n *yaml.Node, budget *int) *yaml.Node {
	if *budget == 0 {
		return nil
	}
	*budget--
	cp := *n
	cp.Content = make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		if cp.Content[i] = copyNode(c, budget); cp.Content[i] == nil {
			return nil
		}
	}
	return &cp
}

// unpublish removes the unpublished parts and the marker keys below n, and
// reports whether it removed anything.
func unpublish(n *yaml.Node) bool {
	removed := false
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			removed = unpublish(c) || removed
		}
	case yaml.SequenceNode:
		kept := n.Content[:0]
		for _, c := range n.Content {
			if marked(c) {
				removed = true
				continue
			}
			removed = unpublish(c) || removed
			kept = append(kept, c)
		}
		n.Content = kept
	case yaml.MappingNode:
		siblings := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if name, ok := strings.CutPrefix(n.Content[i].Value, markerKey+"-"); ok && names(n.Content[i+1], target) {
				siblings[name] = true
			}
		}
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == markerKey || strings.HasPrefix(k.Value, markerKey+"-") || siblings[k.Value] || marked(v) {
				removed = true
				continue
			}
			removed = unpublish(v) || removed
			kept = append(kept, k, v)
		}
		n.Content = kept
	}
	return removed
}

// marked reports whether n is a mapping whose x-doNotPublish value names the
// target.
func marked(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == markerKey && names(n.Content[i+1], target) {
			return true
		}
	}
	return false
}

// names reports whether n is the scalar s or a list holding s.
func names(n *yaml.Node, s string) bool {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value == s
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.Value == s {
				return true
			}
		}
	}
	return false
}
