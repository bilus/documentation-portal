package portal

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// site answers the requests for the pages of one portal.
type site struct {
	config    Portal // the portal as the portal configuration gives it, for the access hook
	whole     *site  // on a reader's site, the site that visibleTo copied, else nil
	root      fs.FS
	name      string // the portal's name
	slug      string // the portal's slug
	sections  []*section
	portals   []*site // the site of every portal, for the portal menu
	hideTryIt bool
	chat      bool   // whether a route of the chat serves the portal's chat page
	base      string // the base path of every URL of the site's pages, or ""
}

// newSite returns the site of the portal p of cfg, with sections, p's
// sections after openSections has checked them, whose URLs it puts under the
// portal's URL path. The site links its chat page when a route of cfg.Chat
// serves that page.
func newSite(cfg Config, p Portal, sections []*section) *site {
	s := &site{config: p, root: cfg.Root, name: p.Name, slug: slugOf(p.Name), sections: sections, hideTryIt: cfg.HideTryIt, base: cfg.BasePath}
	for _, sec := range sections {
		sec.base = cfg.BasePath + "/portals/" + s.slug
	}
	s.chat = slices.ContainsFunc(cfg.Chat, func(r Route) bool { return r.Pattern == "GET "+s.chatPath() })
	return s
}

// url returns the URL that opens the site's portal: the page of its first
// section, or the portal's path without a section.
func (s *site) url() string {
	if len(s.sections) == 0 {
		return (&url.URL{Path: s.base + "/portals/" + s.slug + "/"}).String()
	}
	return s.sections[0].pageURL()
}

// chatPath returns the path of the portal's chat page under the root, as
// the chat's route names it.
func (s *site) chatPath() string {
	return (&url.URL{Path: "/portals/" + s.slug + "/chat"}).String()
}

// chatURL returns the URL of the portal's chat page.
func (s *site) chatURL() string {
	return s.base + s.chatPath()
}

// menu returns the links of the portal menu: the first section of every
// portal under its name, in the order of the portal configuration, or none
// with one portal.
func (s *site) menu() []navLink {
	if len(s.portals) < 2 {
		return nil
	}
	links := make([]navLink, 0, len(s.portals))
	for _, p := range s.portals {
		links = append(links, navLink{Label: p.name, URL: p.url()})
	}
	return links
}

// specFor returns the spec section whose slug is slug, or false.
func (s *site) specFor(slug string) (*section, bool) {
	for _, sec := range s.sections {
		if sec.Type == SpecSection && sec.slug == slug {
			return sec, true
		}
	}
	return nil, false
}

// firstSpec returns the first spec section, or false without one.
func (s *site) firstSpec() (*section, bool) {
	for _, sec := range s.sections {
		if sec.Type == SpecSection {
			return sec, true
		}
	}
	return nil, false
}

// pageURL returns the URL of the section's page: a spec section's viewer
// page, or a docs section's document list.
func (sec *section) pageURL() string {
	if sec.Type == SpecSection {
		return sec.viewerURL("")
	}
	return (&url.URL{Path: sec.base + "/docs/" + sec.slug + "/"}).String()
}

// docURL returns the URL of the document page of the markdown file at doc,
// a path in the docs section's content directory, with query and fragment.
func (sec *section) docURL(doc, query, fragment string) string {
	return (&url.URL{Path: sec.base + "/docs/" + sec.slug + "/" + doc, RawQuery: query, Fragment: fragment}).String()
}

// viewerURL returns the URL of the spec section's viewer page with fragment,
// an operation route or "".
func (sec *section) viewerURL(fragment string) string {
	return (&url.URL{Path: sec.base + "/specs/" + sec.slug, Fragment: fragment}).String()
}

// rawSpecURL returns the URL of the spec section's raw spec.
func (sec *section) rawSpecURL() string {
	return (&url.URL{Path: sec.base + "/api/specs/" + sec.slug}).String()
}

// docsFor returns the docs section whose slug is slug, or false.
func (s *site) docsFor(slug string) (*section, bool) {
	for _, sec := range s.sections {
		if sec.Type == DocsSection && sec.slug == slug {
			return sec, true
		}
	}
	return nil, false
}

// index redirects to the first section's page.
func (s *site) index(w http.ResponseWriter, r *http.Request) {
	if len(s.sections) == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, s.sections[0].pageURL(), http.StatusFound)
}

// viewerPage writes the viewer page of the spec section with the request's
// slug, or an error page for a slug of no spec section and for a missing or
// invalid spec.
func (s *site) viewerPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	sec, ok := s.specFor(slug)
	if !ok {
		render(w, r, http.StatusNotFound, "error.html", page{Title: "Spec not found", Message: "No spec section has the slug " + slug + ".", Nav: s.nav()})
		return
	}
	sp, err := s.loadSpec(sec.Input)
	var invalid invalidSpecError
	switch {
	case errors.Is(err, errNoSpec):
		render(w, r, http.StatusNotFound, "error.html", page{Title: "Spec not found", Message: "No spec at " + sec.Input + ".", Nav: s.nav()})
	case errors.As(err, &invalid):
		render(w, r, http.StatusUnprocessableEntity, "error.html", page{Title: "Cannot show " + sec.Input, Message: invalid.reason, Nav: s.nav()})
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		render(w, r, http.StatusOK, "viewer.html", page{Title: sp.Title, SpecURL: sec.rawSpecURL(), HideTryIt: s.hideTryIt, Nav: s.nav()})
	}
}

type page struct {
	Title     string
	SpecURL   string // viewer page only
	HideTryIt bool   // viewer page only
	Message   string // error page only
	Nav       navBar
	Section   string         // the docs section's title: document pages and lists only
	Sidebar   []sidebarGroup // document pages and lists only
	Contents  []sidebarGroup // document list only: the toc file's groups, or none
	Docs      []docLink      // document list only: the markdown files outside Contents
	Portals   []navLink      // home page only
	Body      template.HTML  // document page only
	Banner    *banner        // above the navigation bar, or nil
	Base      string         // the base path of the assets and the previews link, or ""
}

// docLink links a document page from the document list.
type docLink struct {
	Path string // in the content directory
	URL  string
}

// navBar is the navigation bar of a page.
type navBar struct {
	Links   []navLink
	Portal  string        // the name of the page's portal, the label of the menu
	Menu    []navLink     // the portal menu's links, or none
	Account []AccountLink // the reader's account links, at the bar's right end, or none
}

// navLink is one link of the navigation bar.
type navLink struct {
	Label string
	URL   string
}

// nav returns the navigation bar of the portal's pages: each section's page
// under its title, in the order of the portal configuration, the chat page
// when the portal handler serves one, and the portal menu.
func (s *site) nav() navBar {
	links := make([]navLink, 0, len(s.sections)+1)
	for _, sec := range s.sections {
		links = append(links, navLink{Label: sec.Title, URL: sec.pageURL()})
	}
	if s.chat {
		links = append(links, navLink{Label: "Chat", URL: s.chatURL()})
	}
	return navBar{Links: links, Portal: s.name, Menu: s.menu()}
}

//go:embed templates/*.html
var templateFiles embed.FS

var templates = template.Must(template.ParseFS(templateFiles, "templates/*.html"))

// render writes the named page template with the given status, with the
// account links of r's reader at the right end of the page's navigation bar,
// and above the bar the banner of r's preview, or its notice, which clears
// the notice cookie.
func render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	p.Nav.Account = viewOf(r).account
	p.Banner = bannerOf(r)
	p.Base = baseOf(r)
	if p.Banner != nil && p.Banner.Ended != "" {
		// The notice ends its cookie: the one this response set, and the request's.
		_, err := r.Cookie(endedCookie)
		if dropped := dropCookie(w.Header(), endedCookie); !dropped || err == nil {
			http.SetCookie(w, expired(endedCookie, baseOf(r)))
		}
	}
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// rawSpec writes the raw spec of the spec section with the request's slug,
// or an error for a slug of no spec section and for a missing or invalid
// spec.
func (s *site) rawSpec(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	sec, ok := s.specFor(slug)
	if !ok {
		http.Error(w, "no spec section has the slug "+slug, http.StatusNotFound)
		return
	}
	sp, err := s.loadSpec(sec.Input)
	var invalid invalidSpecError
	switch {
	case errors.Is(err, errNoSpec):
		http.Error(w, "no spec at "+sec.Input, http.StatusNotFound)
	case errors.As(err, &invalid):
		http.Error(w, sec.Input+": "+invalid.reason, http.StatusUnprocessableEntity)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(sp.Raw)
	}
}

// spec is an API spec checked for the viewer.
type spec struct {
	Title string
	Raw   []byte // without its unpublished parts and marker keys
}

// errNoSpec means the request names no spec: a missing file, or a path that
// no spec section names.
var errNoSpec = errors.New("no such spec")

type invalidSpecError struct{ reason string }

func (e invalidSpecError) Error() string { return e.reason }

// loadSpec reads the configured spec at path, if a spec section names it,
// leaves out its unpublished parts, and checks that it is an OpenAPI 3.0 or
// 3.1 document with a title.
func (s *site) loadSpec(path string) (*spec, error) {
	if !slices.ContainsFunc(s.sections, func(sec *section) bool { return sec.Type == SpecSection && sec.Input == path }) {
		return nil, errNoSpec
	}
	raw, err := fs.ReadFile(s.root, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoSpec
	}
	if err != nil {
		return nil, err
	}
	published, err := publishedSpec(raw)
	if err != nil {
		return nil, invalidSpecError{"not valid YAML: " + err.Error()}
	}

	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Swagger string `yaml:"swagger"`
		Info    struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(published, &doc); err != nil {
		return nil, invalidSpecError{"not valid YAML: " + err.Error()}
	}
	switch {
	case doc.Swagger != "":
		return nil, invalidSpecError{"Swagger 2.0 is not supported; convert the spec to OpenAPI 3"}
	case !strings.HasPrefix(doc.OpenAPI, "3.0.") && !strings.HasPrefix(doc.OpenAPI, "3.1."):
		return nil, invalidSpecError{"not an OpenAPI 3.0 or 3.1 document"}
	case doc.Info.Title == "":
		return nil, invalidSpecError{"info.title is missing"}
	}
	return &spec{Title: doc.Info.Title, Raw: published}, nil
}
