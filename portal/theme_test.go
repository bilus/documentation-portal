package portal_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/bilus/documentation-portal/portal"
)

// themed returns the handler of the Pet Shop and Store portals, built with
// opts.
func themed(t *testing.T, cfg portal.Config, opts ...portal.Option) http.Handler {
	t.Helper()
	cfg.Root, cfg.Portals = shopAndStore()
	h, err := portal.New(cfg, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestACustomHomePageListsThePortals(t *testing.T) {
	templates := fstest.MapFS{"home.html": {Data: []byte(`<h1>AutoStage</h1>{{range .Portals}}<a class="card" href="{{.URL}}">{{.Label}}</a>{{end}}`)}}
	h := themed(t, portal.Config{}, portal.WithCustomTemplates(templates))

	home := get(h, "/").Body.String()
	if !strings.Contains(home, `<h1>AutoStage</h1><a class="card" href="/portals/pet-shop/specs/api">Pet Shop</a><a class="card" href="/portals/store/specs/api">Store</a>`) {
		t.Errorf("the home page is not the custom one listing both portals: %s", home)
	}
	if viewer := get(h, "/portals/store/specs/api").Body.String(); !strings.Contains(viewer, "<elements-api") {
		t.Errorf("a page without an override is not the built-in one: %s", viewer)
	}
}

func TestACustomPartialAndAnExtraTemplateReachEveryPage(t *testing.T) {
	templates := fstest.MapFS{
		"nav.html":    {Data: []byte(`{{define "nav"}}<nav class="brand">{{template "logo" .}}{{range .Nav.Links}}<a href="{{.URL}}">{{.Label}}</a>{{end}}</nav>{{end}}`)},
		"extras.html": {Data: []byte(`{{define "logo"}}<img src="{{.Base}}/assets/custom/logo.svg">{{end}}`)},
	}
	h := themed(t, portal.Config{BasePath: "/docs"}, portal.WithCustomTemplates(templates))

	for _, path := range []string{"/docs/portals/store/specs/api", "/docs/portals/pet-shop/docs/guides/", "/docs/portals/pet-shop/docs/guides/a.md", "/docs/portals/pet-shop/docs/guides/missing.md"} {
		page := get(h, path).Body.String()
		if !strings.Contains(page, `<nav class="brand"><img src="/docs/assets/custom/logo.svg">`) || strings.Contains(page, `class="portal-nav"`) {
			t.Errorf("%s: the page lacks the custom navigation bar or keeps the built-in one: %s", path, page)
		}
	}
}

func TestTheCustomTemplatesOfOneHandlerLeaveAnotherAlone(t *testing.T) {
	themed(t, portal.Config{}, portal.WithCustomTemplates(fstest.MapFS{"home.html": {Data: []byte("custom")}}))

	if home := get(themed(t, portal.Config{}), "/").Body.String(); home == "custom" || !strings.Contains(home, "<title>Portals</title>") {
		t.Errorf("a handler without custom templates does not serve the built-in home page: %s", home)
	}
}

func TestNewRefusesCustomTemplatesThatCannotRender(t *testing.T) {
	root, portals := shopAndStore()
	for name, tc := range map[string]struct {
		templates fstest.MapFS
		want      string
	}{
		"a syntax error":             {fstest.MapFS{"home.html": {Data: []byte("{{range .Portals}}")}}, "home.html"},
		"a field the page lacks":     {fstest.MapFS{"list.html": {Data: []byte("{{.Sectoin}}")}}, "Sectoin"},
		"a partial's unknown field":  {fstest.MapFS{"nav.html": {Data: []byte(`{{define "nav"}}{{.Nav.Logo}}{{end}}`)}}, "Logo"},
		"a template it never finds":  {fstest.MapFS{"error.html": {Data: []byte(`{{template "layout" .}}`)}}, "layout"},
		"no templates":               {fstest.MapFS{"logo.svg": {Data: []byte("<svg/>")}}, "no templates"},
		"a field of the portals":     {fstest.MapFS{"home.html": {Data: []byte("{{range .Portals}}{{.Lable}}{{end}}")}}, "Lable"},
		"a field of no portals":      {fstest.MapFS{"home.html": {Data: []byte("{{with .Portals}}{{range .}}{{.Label}}{{end}}{{else}}{{.Mesage}}{{end}}")}}, "Mesage"},
		"a field of an ended banner": {fstest.MapFS{"banner.html": {Data: []byte(`{{define "banner"}}{{with .Banner}}{{if .Ended}}{{.Endded}}{{end}}{{end}}{{end}}`)}}, "Endded"},
		"a field of the viewer":      {fstest.MapFS{"viewer.html": {Data: []byte("{{.SpecUrl}}")}}, "SpecUrl"},
		"a partial without define":   {fstest.MapFS{"nav.html": {Data: []byte(`<nav class="mine"></nav>`)}}, `{{define "nav"}}`},
	} {
		if _, err := portal.New(portal.Config{Root: root, Portals: portals}, portal.WithCustomTemplates(tc.templates)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one naming %s", name, err, tc.want)
		}
	}
}

func TestNewAcceptsTemplatesThatReadWhatEveryRealPageHas(t *testing.T) {
	templates := fstest.MapFS{
		"doc.html":    {Data: []byte("{{(index .Sidebar 0).Title}}")},
		"viewer.html": {Data: []byte("{{(index .Nav.Links 0).URL}}")},
	}
	h := themed(t, portal.Config{}, portal.WithCustomTemplates(templates))

	if page := get(h, "/portals/store/specs/api").Body.String(); page != "/portals/store/specs/api" {
		t.Errorf("the viewer page: %q", page)
	}
}

func TestUnpublishedRefusesCustomTemplatesThatCannotRender(t *testing.T) {
	if _, err := portal.Unpublished(portal.Config{}, portal.WithCustomTemplates(fstest.MapFS{"error.html": {Data: []byte("{{.Mesage}}")}})); err == nil || !strings.Contains(err.Error(), "Mesage") {
		t.Errorf("err = %v, want one naming Mesage", err)
	}
}

func TestCustomAssetsAreServedUnderTheBasePath(t *testing.T) {
	assets := fstest.MapFS{"autostage.css": {Data: []byte("body { color: navy; }")}, "fonts/brand.woff2": {Data: []byte("font")}, ".env": {Data: []byte("X=1")}, ".git/config": {Data: []byte("[core]")}}
	h := themed(t, portal.Config{BasePath: "/docs"}, portal.WithCustomAssets(assets))

	if rec := get(h, "/docs/assets/custom/autostage.css"); rec.Code != http.StatusOK || rec.Body.String() != "body { color: navy; }" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("the stylesheet: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec := get(h, "/docs/assets/custom/fonts/brand.woff2"); rec.Code != http.StatusOK || rec.Body.String() != "font" {
		t.Errorf("a file in a folder: %d %q", rec.Code, rec.Body)
	}
	for _, path := range []string{"/docs/assets/custom/", "/docs/assets/custom/fonts/", "/docs/assets/custom/fonts", "/docs/assets/custom/.env", "/docs/assets/custom/.git/config", "/docs/assets/custom/missing.css"} {
		if rec := get(h, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q, want 404: only files are served, and no hidden ones", path, rec.Code, rec.Body)
		}
	}
	if rec := get(themed(t, portal.Config{}), "/assets/custom/autostage.css"); rec.Code != http.StatusNotFound {
		t.Errorf("without custom assets: %d, want 404", rec.Code)
	}
}

func TestTheUnpublishedPageTakesTheCustomTemplatesAndAssets(t *testing.T) {
	h := unpublished(t, portal.Config{BasePath: "/docs"},
		portal.WithCustomTemplates(fstest.MapFS{"error.html": {Data: []byte(`<link rel="stylesheet" href="{{.Base}}/assets/custom/autostage.css"><h1>{{.Title}}</h1>`)}}),
		portal.WithCustomAssets(fstest.MapFS{"autostage.css": {Data: []byte("body {}")}}))

	if page := get(h, "/docs/").Body.String(); page != `<link rel="stylesheet" href="/docs/assets/custom/autostage.css"><h1>No documentation yet</h1>` {
		t.Errorf("the unpublished page is not the custom error page: %s", page)
	}
	if rec := get(h, "/docs/assets/custom/autostage.css"); rec.Code != http.StatusOK || rec.Body.String() != "body {}" {
		t.Errorf("the stylesheet while unpublished: %d %q", rec.Code, rec.Body)
	}
	if rec := get(h, "/docs//x"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a path with a doubled slash: %d, want the notice", rec.Code)
	}
}
