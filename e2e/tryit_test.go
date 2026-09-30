//go:build e2e

package e2e

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bilus/documentation-portal/portal"
)

func TestViewerTryIt(t *testing.T) {
	browser := newBrowser(t)
	for _, hide := range []bool{false, true} {
		h, err := portal.New(portal.Config{Root: os.DirFS("../testdata/specs"), SpecPath: "petstore-3.1.yaml", HideTryIt: hide})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(h)
		page := srv.URL + "/specs/petstore-3.1.yaml#/operations/showPetById"
		if hide {
			if text := pageText(t, browser, page, "microchipId"); strings.Contains(text, "Send API Request") {
				t.Error("the Try It console shows with HideTryIt")
			}
		} else {
			pageText(t, browser, page, "Send API Request")
		}
		srv.Close()
	}
}
