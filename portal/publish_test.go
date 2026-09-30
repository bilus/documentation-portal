package portal

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublishedSpec(t *testing.T) {
	for _, tc := range []struct {
		name, spec    string
		gone, present []string
	}{
		{
			name: "mapping",
			spec: "components:\n  schemas:\n    Pet:\n      properties:\n        shownField:\n          type: string\n        unpublishedField:\n          type: string\n          x-doNotPublish:\n            - main\n",
			gone: []string{"unpublishedField"}, present: []string{"shownField"},
		},
		{
			name: "list element",
			spec: "tags:\n  - name: shownTag\n  - name: unpublishedTag\n    x-doNotPublish:\n      - main\n",
			gone: []string{"unpublishedTag", "{}"}, present: []string{"shownTag"},
		},
		{
			name: "sibling scalar",
			spec: "info:\n  title: Pets\n  x-doNotPublish-description:\n    - main\n  description: unpublishedText\n",
			gone: []string{"unpublishedText", "description"}, present: []string{"Pets"},
		},
		{
			name: "sibling before its marker key",
			spec: "info:\n  title: Pets\n  description: unpublishedText\n  x-doNotPublish-description:\n    - main\n",
			gone: []string{"unpublishedText", "description"}, present: []string{"Pets"},
		},
		{
			name: "sibling list",
			spec: "Pet:\n  type: object\n  x-doNotPublish-required:\n    - main\n  required:\n    - unpublishedItem\n",
			gone: []string{"unpublishedItem", "required"}, present: []string{"object"},
		},
		{
			name: "marked part inside a kept list element",
			spec: "parameters:\n  - name: shownParam\n    schema:\n      properties:\n        shownProp:\n          type: string\n        unpublishedProp:\n          type: string\n          x-doNotPublish:\n            - main\n",
			gone: []string{"unpublishedProp"}, present: []string{"shownParam", "shownProp"},
		},
		{
			name:    "mapping for another target",
			spec:    "Pet:\n  properties:\n    betaField:\n      type: string\n      x-doNotPublish:\n        - beta\n",
			present: []string{"betaField"},
		},
		{
			name:    "list element for another target",
			spec:    "tags:\n  - name: betaTag\n    x-doNotPublish:\n      - beta\n",
			present: []string{"betaTag"},
		},
		{
			name:    "sibling for another target",
			spec:    "info:\n  x-doNotPublish-description:\n    - beta\n  description: betaText\n",
			present: []string{"betaText", "description"},
		},
		{
			name: "two targets",
			spec: "Pet:\n  properties:\n    unpublishedField:\n      type: string\n      x-doNotPublish:\n        - beta\n        - main\n    shownField:\n      type: string\n",
			gone: []string{"unpublishedField"}, present: []string{"shownField"},
		},
		{
			name:    "a target that contains main",
			spec:    "Pet:\n  properties:\n    domainField:\n      type: string\n      x-doNotPublish:\n        - domain\n",
			present: []string{"domainField"},
		},
		{
			name: "scalar marker",
			spec: "Pet:\n  properties:\n    unpublishedField:\n      type: string\n      x-doNotPublish: main\n    shownField:\n      type: string\n",
			gone: []string{"unpublishedField"}, present: []string{"shownField"},
		},
		{
			name:    "mapping as a marker",
			spec:    "Pet:\n  properties:\n    keptField:\n      type: string\n      x-doNotPublish:\n        main: true\n",
			present: []string{"keptField"},
		},
		{
			name: "two marker keys",
			spec: "Pet:\n  properties:\n    unpublishedField:\n      x-doNotPublish:\n        - beta\n      type: string\n      x-doNotPublish:\n        - main\n    shownField:\n      type: string\n",
			gone: []string{"unpublishedField"}, present: []string{"shownField"},
		},
		{
			name: "escaped marker key",
			spec: "Pet:\n  properties:\n    unpublishedField:\n      type: string\n      \"x-doNot\\x50ublish\":\n        - main\n    shownField:\n      type: string\n",
			gone: []string{"unpublishedField", `x-doNot\x50ublish`}, present: []string{"shownField"},
		},
		{
			name: "marker list through an alias",
			spec: "x-targets: &t\n  - main\nsecret:\n  x-doNotPublish: *t\n  value: unpublishedValue\nkept: shownValue\n",
			gone: []string{"unpublishedValue", "secret"}, present: []string{"shownValue"},
		},
		{
			name: "alias to an unpublished anchor",
			spec: "Secret: &secret\n  type: object\n  field: unpublishedValue\n  x-doNotPublish:\n    - main\nCopy: *secret\nShown: shownValue\n",
			gone: []string{"unpublishedValue", "Secret", "Copy"}, present: []string{"shownValue"},
		},
		{
			name: "alias to a mapping with an unpublished part",
			spec: "Base: &base\n  x-doNotPublish-secret:\n    - main\n  secret: unpublishedValue\n  shown: shownValue\nCopy: *base\n",
			gone: []string{"unpublishedValue", "&base", "*base"}, present: []string{"shownValue", "Copy"},
		},
		{
			name:    "marker on the root",
			spec:    "x-doNotPublish:\n  - main\nopenapi: 3.1.0\ninfo:\n  title: Pets\n",
			present: []string{"openapi", "Pets"},
		},
		{
			name: "several documents",
			spec: "a: shownA\nx-doNotPublish-b:\n  - main\nb: unpublishedB\n---\nc: shownC\n",
			gone: []string{"unpublishedB"}, present: []string{"shownA", "shownC"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := publishedSpec([]byte(tc.spec))
			if err != nil {
				t.Fatal(err)
			}
			dec := yaml.NewDecoder(bytes.NewReader(got))
			for {
				var doc yaml.Node
				if err := dec.Decode(&doc); err != nil {
					if err.Error() != "EOF" {
						t.Fatalf("the result is not YAML: %v\n%s", err, got)
					}
					break
				}
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
}

func TestPublishedSpecKeepsBytesWhenNothingIsRemoved(t *testing.T) {
	for _, spec := range []string{
		"# A comment the portal keeps.\nopenapi: 3.1.0\ninfo: {title: Pets}\n",
		"openapi:    3.1.0\ninfo:\n    title:   Pets\n",
		"# x-doNotPublish is not used in this file.\nopenapi:    3.1.0\ninfo:\n    title:   Pets\n",
	} {
		if got, err := publishedSpec([]byte(spec)); err != nil || string(got) != spec {
			t.Errorf("%q came back as %q, %v", spec, got, err)
		}
	}
}

func TestPublishedSpecRejectsExcessiveAliasing(t *testing.T) {
	for _, spec := range []string{
		"x-doNotPublish: [main]\n" +
			"l0: &l0 [x, x, x, x, x, x, x, x, x, x]\n" +
			"l1: &l1 [*l0, *l0, *l0, *l0, *l0, *l0, *l0, *l0, *l0, *l0]\n" +
			"l2: &l2 [*l1, *l1, *l1, *l1, *l1, *l1, *l1, *l1, *l1, *l1]\n" +
			"l3: &l3 [*l2, *l2, *l2, *l2, *l2, *l2, *l2, *l2, *l2, *l2]\n" +
			"l4: &l4 [*l3, *l3, *l3, *l3, *l3, *l3, *l3, *l3, *l3, *l3]\n" +
			"l5: &l5 [*l4, *l4, *l4, *l4, *l4, *l4, *l4, *l4, *l4, *l4]\n",
		"x-doNotPublish: [main]\nloop: &loop [*loop]\n",
	} {
		if _, err := publishedSpec([]byte(spec)); err == nil {
			t.Errorf("expanded without an error:\n%s", spec)
		}
	}
}
