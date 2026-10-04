package portal

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadPreviewRequest(t *testing.T) {
	for _, tc := range []struct {
		target, cookie string
		want           previewRequest
	}{
		{"/portals/pets/specs/api", "", previewRequest{}},
		{"/previews/", "", previewRequest{leave: true}},
		{"/previews", "portal-preview=pr-1", previewRequest{leave: true, folder: "pr-1"}},
		{"/previews/pr-1", "", previewRequest{enter: "pr-1", path: "/"}},
		{"/previews/pr-1/portals/pets/docs/guides/", "", previewRequest{enter: "pr-1", path: "/portals/pets/docs/guides/"}},
		{"/previews/a%2Fb/x%20y.md", "", previewRequest{enter: "a/b", path: "/x%20y.md"}},
		{"/previews/pr-1//evil.example", "", previewRequest{enter: "pr-1", path: "/evil.example"}},
		{"/previews/pr-1/../../x", "", previewRequest{enter: "pr-1", path: "/x"}},
		{"/previews//x", "", previewRequest{}},
		{"/previewsx/pr-1", "", previewRequest{}},
		{"/portals/pets/specs/api", "portal-preview=pr-1; portal-preview-ended=pr-0", previewRequest{folder: "pr-1", ended: "pr-0"}},
		{"/portals/pets/specs/api", "portal-preview=..; portal-preview-ended=a%20b", previewRequest{}},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.target, nil)
		r.Header.Set("Cookie", tc.cookie)
		if got := readPreviewRequest(r); got != tc.want {
			t.Errorf("%s with the cookies %q: %+v, want %+v", tc.target, tc.cookie, got, tc.want)
		}
	}
}

func TestPreviewsReaderAccess(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })
	asked := 0
	p := &previews{access: func(*http.Request) (Access, error) {
		asked++
		return Everything, nil
	}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, req := range []previewRequest{{}, {leave: true, folder: "pr-1"}, {ended: "pr-0"}} {
		if access := p.readerAccess(r, req); access != nil {
			t.Errorf("%+v: %v, want no access", req, access)
		}
	}
	if asked != 0 {
		t.Errorf("requests without a folder asked the hook %d times", asked)
	}
	for _, req := range []previewRequest{{enter: "pr-1"}, {folder: "pr-1"}} {
		if access := p.readerAccess(r, req); access != Everything {
			t.Errorf("%+v: %v, want the hook's access", req, access)
		}
	}
	if asked != 2 {
		t.Errorf("two requests with a folder asked the hook %d times", asked)
	}
	if access := (&previews{}).readerAccess(r, previewRequest{enter: "pr-1"}); access != Everything {
		t.Errorf("without a hook: %v, want Everything", access)
	}
	for name, hook := range map[string]func(*http.Request) (Access, error){
		"an error":  func(*http.Request) (Access, error) { return Everything, errors.New("the token expired") },
		"no access": func(*http.Request) (Access, error) { return nil, nil },
	} {
		access := (&previews{access: hook}).readerAccess(r, previewRequest{enter: "pr-1"})
		if _, ok := access.(nothing); !ok {
			t.Errorf("%s: %v, want an access that allows nothing", name, access)
		}
	}
	if !strings.Contains(logged.String(), "access hook: the token expired") || !strings.Contains(logged.String(), "access hook: no access") {
		t.Errorf("the log: %q", logged.String())
	}
}

// sectionsOnly is an access without the method Preview.
type sectionsOnly struct{}

func (sectionsOnly) Portal(Portal) bool           { return true }
func (sectionsOnly) Section(Portal, Section) bool { return true }

func TestOpenFolder(t *testing.T) {
	var opened []string
	p := &previews{open: func(_ context.Context, folder string) (http.Handler, error) {
		opened = append(opened, folder)
		switch folder {
		case "pr-1":
			return http.NotFoundHandler(), nil
		case "none":
			return nil, nil
		}
		return nil, errors.New("bucket unreachable")
	}}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, req := range []previewRequest{{enter: "pr-1"}, {folder: "pr-1"}} {
		if h, err := p.openFolder(r, req, Everything); err != nil || h == nil {
			t.Errorf("%+v: %v, %v, want the handler of pr-1", req, h, err)
		}
	}
	// A switch opens its own folder, not the reader's preview.
	if _, err := p.openFolder(r, previewRequest{enter: "pr-2", folder: "pr-1"}, Everything); err == nil || errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), `preview "pr-2": bucket unreachable`) {
		t.Errorf("the switch to pr-2: %v, want its failed load", err)
	}
	if _, err := p.openFolder(r, previewRequest{enter: "none"}, Everything); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an opener without a handler or an error: %v, want an error", err)
	}
	opened = nil
	for name, tc := range map[string]struct {
		req    previewRequest
		access Access
	}{
		"an invalid name":            {previewRequest{enter: ".."}, Everything},
		"an access without Preview":  {previewRequest{enter: "pr-1"}, sectionsOnly{}},
		"an access that allows none": {previewRequest{folder: "pr-1"}, nothing{}},
		"no access":                  {previewRequest{enter: "pr-1"}, nil},
	} {
		if h, err := p.openFolder(r, tc.req, tc.access); h != nil || !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: %v, %v, want an error of fs.ErrNotExist", name, h, err)
		}
	}
	if len(opened) != 0 {
		t.Errorf("folders hidden from the reader were opened: %v", opened)
	}
	for _, req := range []previewRequest{{}, {leave: true, folder: "pr-1"}, {ended: "pr-0"}} {
		if h, err := p.openFolder(r, req, Everything); h != nil || err != nil {
			t.Errorf("%+v: %v, %v, want no folder to open", req, h, err)
		}
	}
}

func TestRenderShowsTheBanner(t *testing.T) {
	for name, tc := range map[string]struct {
		banner banner
		want   string
		clears bool // the notice cookie
	}{
		"a preview": {banner{Preview: "pr-1"}, `<div class="portal-banner">Preview <strong>pr-1</strong><a href="/previews/">Leave the preview</a></div>`, false},
		"a notice":  {banner{Ended: "pr-0"}, `<div class="portal-banner">The preview <strong>pr-0</strong> is no longer available. This is the published documentation.</div>`, true},
		"none":      {banner{}, "", false},
	} {
		rec := httptest.NewRecorder()
		render(rec, withBanner(httptest.NewRequest(http.MethodGet, "/", nil), tc.banner), http.StatusOK, "error.html", page{Title: "T", Message: "M"})
		body := rec.Body.String()
		if tc.want != "" && !strings.Contains(body, tc.want) || tc.want == "" && strings.Contains(body, "portal-banner") {
			t.Errorf("%s: %q, want %q", name, body, tc.want)
		}
		cleared := strings.Contains(rec.Header().Get("Set-Cookie"), "portal-preview-ended=; Path=/; Max-Age=0")
		if cleared != tc.clears {
			t.Errorf("%s: the notice cookie cleared %v, want %v", name, cleared, tc.clears)
		}
	}
}

func TestValidFolder(t *testing.T) {
	for _, name := range []string{"pr-1", "PR_1.x", "a.b-c", "0"} {
		if !validFolder(name) {
			t.Errorf("%q is refused", name)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "a b", "a%2Fb", "a[b", "a\\b", "a]b", "a^b", "a`b", "a~b", "caf\u00e9"} {
		if validFolder(name) {
			t.Errorf("%q is accepted", name)
		}
	}
}
