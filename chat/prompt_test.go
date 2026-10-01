package chat

import (
	"strings"
	"testing"
)

func TestAPIs(t *testing.T) {
	for _, tc := range []struct {
		titles []string
		want   string
	}{
		{nil, ""},
		{[]string{"Pets"}, "the Pets API"},
		{[]string{"Pets", "Store"}, "the Pets and Store APIs"},
		{[]string{"Pets", "Store", "Users"}, "the Pets, Store and Users APIs"},
	} {
		if got := apis(tc.titles); got != tc.want {
			t.Errorf("apis(%q) = %q, want %q", tc.titles, got, tc.want)
		}
	}
}

func TestInstructionNamesEveryAPI(t *testing.T) {
	for _, tc := range []struct {
		titles []string
		want   []string
	}{
		{nil, []string{"about this API. Your tools read its published documentation: the API reference and the guides.", "Stay on the subject of this API,"}},
		{[]string{"Pets"}, []string{"about the Pets API. Your tools read its published documentation: the API reference and the guides.", "Stay on the subject of this API,"}},
		{[]string{"Pets", "Store"}, []string{"about the Pets and Store APIs. Your tools read their published documentation: the API references and the guides.", "Stay on the subject of these APIs,"}},
	} {
		got := instruction(tc.titles)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%q: the prompt lacks %q:\n%s", tc.titles, want, got)
			}
		}
	}
}
