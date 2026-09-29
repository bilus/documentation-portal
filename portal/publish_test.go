package portal

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublishedSpec(t *testing.T) {
	t.Skip("HOLE(1): drop the parts that x-doNotPublish marks for main")
	for _, tc := range []struct {
		name, spec    string
		gone, present []string
	}{
		{
			name: "mapping",
			spec: "components:\n  schemas:\n    Pet:\n      properties:\n        shownField:\n          type: string\n        hiddenField:\n          type: string\n          x-doNotPublish:\n            - main\n",
			gone: []string{"hiddenField"}, present: []string{"shownField"},
		},
		{
			name: "list element",
			spec: "tags:\n  - name: shownTag\n  - name: hiddenTag\n    x-doNotPublish:\n      - main\n",
			gone: []string{"hiddenTag"}, present: []string{"shownTag"},
		},
		{
			name: "sibling scalar",
			spec: "info:\n  title: Pets\n  x-doNotPublish-description:\n    - main\n  description: hiddenText\n",
			gone: []string{"hiddenText", "description"}, present: []string{"Pets"},
		},
		{
			name: "sibling list",
			spec: "Pet:\n  type: object\n  x-doNotPublish-required:\n    - main\n  required:\n    - hiddenItem\n",
			gone: []string{"hiddenItem", "required"}, present: []string{"object"},
		},
		{
			name:    "another target",
			spec:    "Pet:\n  properties:\n    betaField:\n      type: string\n      x-doNotPublish:\n        - beta\n",
			present: []string{"betaField"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := publishedSpec([]byte(tc.spec))
			if err != nil {
				t.Fatal(err)
			}
			var doc yaml.Node
			if err := yaml.Unmarshal(got, &doc); err != nil {
				t.Fatalf("the result is not YAML: %v\n%s", err, got)
			}
			for _, s := range append(tc.gone, "x-doNotPublish") {
				if strings.Contains(string(got), s) {
					t.Errorf("the result keeps %s:\n%s", s, got)
				}
			}
			for _, s := range tc.present {
				if !strings.Contains(string(got), s) {
					t.Errorf("the result lost %s:\n%s", s, got)
				}
			}
		})
	}

	unmarked := []byte("# A comment the portal keeps.\nopenapi: 3.1.0\ninfo: {title: Pets}\n")
	if got, err := publishedSpec(unmarked); err != nil || !bytes.Equal(got, unmarked) {
		t.Errorf("a spec without markers changed: %q, %v", got, err)
	}
}
