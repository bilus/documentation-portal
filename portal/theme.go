package portal

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// WithCustomTemplates renders the pages with the html/template files at the
// top of fsys, named *.html, in place of the built-in ones of the same name:
// home.html, doc.html, list.html, viewer.html and error.html, and the
// partials every page calls, nav.html, banner.html and sidebar.html. A page
// or partial without a file of its own keeps the built-in one, and a file of
// another name adds its templates for the others to call. New refuses
// templates that do not parse, and those that fail to render a page of sample
// data, such as one that names a field the page lacks.
func WithCustomTemplates(fsys fs.FS) Option {
	return func(o *renderOptions) { o.templates = fsys }
}

// WithCustomAssets serves the files of fsys, such as the stylesheets, images
// and fonts of custom templates, at /assets/custom/ under the base path,
// where a template links them as {{.Base}}/assets/custom/name. Only files are
// served: a directory, a path with a segment starting with a dot, such as
// .env, and a path that fsys cannot open get 404, so a program serving a
// directory passes os.Root's FS to keep symlinks inside it. They are served
// to every request that reaches the portal handler, as the Elements assets
// are.
func WithCustomAssets(fsys fs.FS) Option {
	return func(o *renderOptions) { o.assets = fsys }
}

// pageNames are the templates of the pages, which render calls by name.
var pageNames = []string{"home.html", "doc.html", "list.html", "viewer.html", "error.html"}

// partialNames are the templates every page calls, each defined in the file
// of its name with .html.
var partialNames = []string{"nav", "banner", "sidebar"}

// pageTemplates returns the built-in templates without custom ones, or the
// built-in templates with the custom ones of fsys parsed over them, refused
// when fsys holds none, when one does not parse, when the file of a partial
// does not define it, and when a page fails to render one of its sample
// pages.
func pageTemplates(fsys fs.FS) (*template.Template, error) {
	if fsys == nil {
		return templates, nil
	}
	names, err := fs.Glob(fsys, "*.html")
	if err != nil {
		return nil, fmt.Errorf("custom templates: %w", err)
	}
	if len(names) == 0 {
		return nil, errors.New("custom templates: no templates (*.html) at the top of the directory")
	}
	for _, partial := range partialNames {
		if err := checkPartial(fsys, partial); err != nil {
			return nil, fmt.Errorf("custom templates: %w", err)
		}
	}
	t, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if t, err = t.ParseFS(fsys, "*.html"); err != nil {
		return nil, fmt.Errorf("custom templates: %w", err)
	}
	for _, name := range pageNames {
		for _, p := range samplesOf(name) {
			if err := t.ExecuteTemplate(io.Discard, name, p); err != nil {
				return nil, fmt.Errorf("custom templates: %w", err)
			}
		}
	}
	return t, nil
}

// checkPartial refuses the file of the partial in fsys when it does not
// define the partial, since every page would keep the built-in one; it
// accepts fsys without the file.
func checkPartial(fsys fs.FS, partial string) error {
	file := partial + ".html"
	src, err := fs.ReadFile(fsys, file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := template.New(file).Parse(string(src))
	if err != nil {
		return err
	}
	if t.Lookup(partial) == nil {
		return fmt.Errorf(`%s does not define its partial: wrap it in {{define %q}}…{{end}}`, file, partial)
	}
	return nil
}

// samplesOf returns the sample pages of the page template name: each page a
// request can bring, with every banner and with and without account links,
// so that each branch of the page's conditions that a request can take runs.
func samplesOf(name string) []page {
	sidebar := []sidebarGroup{{Links: []sidebarLink{{Title: "a.md", URL: "a.md", Current: true}}}, {Title: "More", Links: []sidebarLink{{Title: "b.md", URL: "b.md"}}}}
	nav := navBar{
		Links:  []navLink{{Label: "API", URL: "/docs/portals/pets/specs/api"}, {Label: "Guides", URL: "/docs/portals/pets/docs/guides/"}},
		Portal: "Pets",
		Menu:   []navLink{{Label: "Pets", URL: "/docs/portals/pets/specs/api"}, {Label: "Store", URL: "/docs/portals/store/specs/api"}},
	}
	alone := nav
	alone.Menu = nil
	var pages []page
	switch name {
	case "home.html":
		pages = []page{
			{Title: "Portals", Portals: []navLink{{Label: "Pets", URL: "/docs/portals/pets/specs/api"}, {Label: "Store", URL: "/docs/portals/store/specs/api"}}},
			{Title: "Portals", Message: "No portals are open to you."},
		}
	case "doc.html":
		pages = []page{
			{Title: "a.md", Section: "Guides", Sidebar: sidebar, Body: "<p>Body</p>", CodeCSS: ".portal-code pre{}", Nav: nav},
			{Title: "a.md", Section: "Guides", Sidebar: sidebar, Body: "<p>Body</p>", Nav: alone},
		}
	case "list.html":
		pages = []page{
			{Title: "Guides", Section: "Guides", Sidebar: sidebar, Contents: sidebar, Nav: nav},
			{Title: "Guides", Section: "Guides", Sidebar: sidebar, Docs: []docLink{{Path: "a.md", URL: "a.md"}}, Nav: alone},
		}
	case "viewer.html":
		pages = []page{
			{Title: "Pets", SpecURL: "/docs/portals/pets/api/specs/api", HideTryIt: true, Nav: nav},
			{Title: "Pets", SpecURL: "/docs/portals/pets/api/specs/api", Nav: alone},
		}
	case "error.html":
		pages = []page{
			{Title: "Document not found", Message: "No markdown file at a.md.", Nav: nav},
			{Title: "Portal not found", Message: "No portal has the slug pets.", Nav: navBar{Links: []navLink{{Label: "Portals", URL: "/docs/"}}}},
		}
	}
	var samples []page
	for _, p := range pages {
		for _, b := range []*banner{nil, {Preview: "pr-1"}, {Ended: "pr-1"}} {
			for _, account := range [][]AccountLink{nil, {{Label: "Ada"}, {Label: "Sign out", URL: "/docs/auth/logout"}}} {
				p.Banner, p.Nav.Account, p.Base = b, account, "/docs"
				samples = append(samples, p)
			}
		}
	}
	return samples
}

// customAssets serves the files of fsys at /assets/custom/: a file, or a
// symlink to one that fsys follows, by its path under /assets/custom/, and
// 404 for a directory, a path with a segment starting with a dot, and any
// path fsys cannot open, such as one that leaves it.
func customAssets(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/assets/custom/")
		f, info, ok := openAsset(fsys, name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		content, ok := f.(io.ReadSeeker)
		if !ok {
			data, err := io.ReadAll(f)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			content = bytes.NewReader(data)
		}
		http.ServeContent(w, r, name, info.ModTime(), content)
	})
}

// openAsset opens the file name of fsys, or returns false for a name that is
// not a valid path, has a segment starting with a dot, or does not open to a
// regular file.
func openAsset(fsys fs.FS, name string) (fs.File, fs.FileInfo, bool) {
	if !fs.ValidPath(name) || name == "." {
		return nil, nil, false
	}
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return nil, nil, false
		}
	}
	f, err := fsys.Open(name)
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, false
	}
	return f, info, true
}
