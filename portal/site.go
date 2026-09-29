package portal

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// site answers the requests for the configured spec and the content directory.
type site struct {
	specs    fs.FS
	specPath string
	docs     fs.FS // nil without a content directory
}

func (s *site) index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, (&url.URL{Path: "/specs/" + s.specPath}).String(), http.StatusFound)
}

func (s *site) viewerPage(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	sp, err := s.loadSpec(path)
	var invalid invalidSpecError
	switch {
	case errors.Is(err, errNoSpec):
		render(w, http.StatusNotFound, "error.html", page{Title: "Spec not found", Message: "No spec at " + path + "."})
	case errors.As(err, &invalid):
		render(w, http.StatusUnprocessableEntity, "error.html", page{Title: "Cannot show " + path, Message: invalid.reason})
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		render(w, http.StatusOK, "viewer.html", page{Title: sp.Title, SpecURL: (&url.URL{Path: "/api/specs/" + path}).String(), Nav: s.nav()})
	}
}

type page struct {
	Title   string
	SpecURL string // viewer page only
	Message string // error page only
	Nav     []navLink
	Sidebar []sidebarGroup // document pages only
}

// navLink is one link of the navigation bar.
type navLink struct {
	Label string
	URL   string
}

// nav returns the links of the navigation bar: the viewer page, and the
// document list when a content directory is configured.
func (s *site) nav() []navLink {
	// HOLE(3): link the viewer page, and the document list when s.docs is set
	return nil
}

//go:embed templates/*.html
var templateFiles embed.FS

var templates = template.Must(template.ParseFS(templateFiles, "templates/*.html"))

// render writes the named page template with the given status.
func render(w http.ResponseWriter, status int, name string, p page) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *site) rawSpec(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	sp, err := s.loadSpec(path)
	var invalid invalidSpecError
	switch {
	case errors.Is(err, errNoSpec):
		http.Error(w, "no spec at "+path, http.StatusNotFound)
	case errors.As(err, &invalid):
		http.Error(w, path+": "+invalid.reason, http.StatusUnprocessableEntity)
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
	Raw   []byte
}

// errNoSpec means the request names no spec: a missing file, or any path
// other than the configured spec.
var errNoSpec = errors.New("no such spec")

type invalidSpecError struct{ reason string }

func (e invalidSpecError) Error() string { return e.reason }

// loadSpec reads the configured spec, if path names it, and checks that it is
// an OpenAPI 3.0 or 3.1 document with a title.
func (s *site) loadSpec(path string) (*spec, error) {
	if path != s.specPath {
		return nil, errNoSpec
	}
	raw, err := fs.ReadFile(s.specs, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoSpec
	}
	if err != nil {
		return nil, err
	}

	var doc struct {
		OpenAPI string `yaml:"openapi"`
		Swagger string `yaml:"swagger"`
		Info    struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
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
	return &spec{Title: doc.Info.Title, Raw: raw}, nil
}
