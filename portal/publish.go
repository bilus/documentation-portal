package portal

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// marker marks the parts of a spec that a publication target leaves out.
const marker = "x-doNotPublish"

// target is the publication target whose unpublished parts the portal drops.
const target = "main"

// publishedSpec returns raw without its unpublished parts and marker keys, or
// raw itself when it has no marker.
func publishedSpec(raw []byte) ([]byte, error) {
	if !bytes.Contains(raw, []byte(marker)) {
		return raw, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	unpublish(&doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unpublish removes the unpublished parts and the marker keys below n.
func unpublish(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			unpublish(c)
		}
	case yaml.SequenceNode:
		kept := n.Content[:0]
		for _, c := range n.Content {
			if !marked(c) {
				unpublish(c)
				kept = append(kept, c)
			}
		}
		n.Content = kept
	case yaml.MappingNode:
		// A key x-doNotPublish-<name> marks its sibling <name>.
		hidden := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if name, ok := strings.CutPrefix(n.Content[i].Value, marker+"-"); ok && names(n.Content[i+1], target) {
				hidden[name] = true
			}
		}
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == marker || strings.HasPrefix(k.Value, marker+"-") || hidden[k.Value] || marked(v) {
				continue
			}
			unpublish(v)
			kept = append(kept, k, v)
		}
		n.Content = kept
	}
}

// marked reports whether n is a mapping whose marker names the target.
func marked(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == marker {
			return names(n.Content[i+1], target)
		}
	}
	return false
}

// names reports whether n is s, or a list that holds s.
func names(n *yaml.Node, s string) bool {
	if n.Kind == yaml.ScalarNode {
		return n.Value == s
	}
	for _, c := range n.Content {
		if c.Kind == yaml.ScalarNode && c.Value == s {
			return true
		}
	}
	return false
}
