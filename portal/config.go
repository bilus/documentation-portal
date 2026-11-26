package portal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Section is a spec or a content directory of the documentation root, or a
// link to a page that the program or another site serves, shown under its
// title in the navigation bar.
type Section struct {
	Title  string      `yaml:"title"`
	Type   SectionType `yaml:"type"`
	Input  string      `yaml:"input"`  // the spec path or the content directory path, relative to Root
	Toc    string      `yaml:"toc"`    // a docs section's toc path, relative to Root, or empty
	URL    string      `yaml:"url"`    // a link section's URL: a path from the server's root, or an http or https URL
	Labels []string    `yaml:"labels"` // for the access hook's rules; the portal reads none
}

// SectionType says what a section's input names, or that its URL names its
// page.
type SectionType string

const (
	SpecSection SectionType = "spec" // an API spec
	DocsSection SectionType = "docs" // a content directory
	LinkSection SectionType = "link" // a page outside the portal handler
)

// Portal is a named set of sections, whose pages have URLs of their own under
// /portals/{slug}/, where the slug comes from the name.
type Portal struct {
	Name     string    `yaml:"name"`
	Sections []Section `yaml:"sections"`
	Labels   []string  `yaml:"labels"` // for the access hook's rules and the collections' match labels
}

// Collection is a named set of portals, listed under its name on the home page
// and in the portal menu: the portals whose labels hold every match label.
type Collection struct {
	Name        string   `yaml:"name"`
	MatchLabels []string `yaml:"matchLabels"`
}

// ReadConfig reads the portals and the collections from the configuration
// file at name, a path inside root, into the portal configuration of root. It
// refuses no root, a file that is missing, that is not YAML or that holds
// more than one YAML document, a key it does not know, and an empty entry in
// a list of portals, of sections, of collections or of match labels. An
// empty file gives no portals. New checks the portals and the collections.
func ReadConfig(root fs.FS, name string) (Config, error) {
	if root == nil {
		return Config{}, errors.New("no documentation root")
	}
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration file: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// Pointers keep an empty entry, which yaml.v3 drops from a list of structs.
	var file struct {
		Portals []*struct {
			Name     string     `yaml:"name"`
			Sections []*Section `yaml:"sections"`
			Labels   []string   `yaml:"labels"`
		} `yaml:"portals"`
		// Pointers keep a null match label, which yaml.v3 drops from a list of strings.
		Collections []*struct {
			Name        string    `yaml:"name"`
			MatchLabels []*string `yaml:"matchLabels"`
		} `yaml:"collections"`
	}
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("configuration file %s: %w", name, err)
	}
	switch err := dec.Decode(new(yaml.Node)); {
	case err == nil:
		return Config{}, fmt.Errorf("configuration file %s holds more than one YAML document", name)
	case !errors.Is(err, io.EOF):
		return Config{}, fmt.Errorf("configuration file %s: %w", name, err)
	}
	var portals []Portal
	for i, p := range file.Portals {
		if p == nil {
			return Config{}, fmt.Errorf("configuration file %s: portal %d is empty", name, i+1)
		}
		var sections []Section
		for j, sec := range p.Sections {
			if sec == nil {
				return Config{}, fmt.Errorf("configuration file %s: portal %q: section %d is empty", name, p.Name, j+1)
			}
			sections = append(sections, *sec)
		}
		portals = append(portals, Portal{Name: p.Name, Sections: sections, Labels: p.Labels})
	}
	var collections []Collection
	for i, g := range file.Collections {
		if g == nil {
			return Config{}, fmt.Errorf("configuration file %s: collection %d is empty", name, i+1)
		}
		var labels []string
		for j, label := range g.MatchLabels {
			if label == nil {
				return Config{}, fmt.Errorf("configuration file %s: collection %q: match label %d is empty", name, g.Name, j+1)
			}
			labels = append(labels, *label)
		}
		collections = append(collections, Collection{Name: g.Name, MatchLabels: labels})
	}
	return Config{Root: root, Portals: portals, Collections: collections}, nil
}

// openPortals checks the portals of cfg and their sections and opens the
// content directory of each docs section, so that every input and toc path
// stays inside the documentation root. It returns a site for each portal, in
// order, and refuses no portals, a portal without a name, with an empty slug
// or with the slug of another portal, and the sections that openSections
// refuses.
func openPortals(cfg Config) ([]*site, error) {
	if cfg.Root == nil {
		return nil, errors.New("no documentation root")
	}
	if len(cfg.Portals) == 0 {
		return nil, errors.New("no portals")
	}
	sites := make([]*site, 0, len(cfg.Portals))
	names := map[string]string{} // the name of the portal with each slug
	for i, p := range cfg.Portals {
		slug := slugOf(p.Name)
		switch other, taken := names[slug]; {
		case p.Name == "":
			return nil, fmt.Errorf("portal %d has no name", i+1)
		case slug == "":
			return nil, fmt.Errorf("portal %q: the name gives an empty slug", p.Name)
		case taken:
			return nil, fmt.Errorf("portals %q and %q share the slug %q", other, p.Name, slug)
		}
		names[slug] = p.Name
		sections, err := openSections(cfg.Root, p.Sections)
		if err != nil {
			return nil, fmt.Errorf("portal %q: %w", p.Name, err)
		}
		sites = append(sites, newSite(cfg, p, sections))
	}
	for _, s := range sites {
		s.portals = sites
	}
	return sites, nil
}

// section is a section of the portal configuration after openSections has
// checked it, with its slug and, for a docs section, its content directory.
type section struct {
	Section
	config Section // the section as the portal configuration gives it, for the access hook
	slug   string
	base   string // the URL path of its portal, such as /portals/pets, or empty
	docs   fs.FS  // a docs section's content directory handle, else nil
	link   string // a link section's URL as linkURL wrote it, else empty

	tocMu      sync.Mutex
	tocProblem string // the toc file's problem that the log named last, or empty
}

// openSections checks sections, the sections of a portal, and opens the
// content directory of each docs section in root, so that every input and
// toc path stays inside root, the documentation root. It cleans a leading ./
// and a trailing / from each path, and refuses no documentation root, no
// sections, a section without a title, with an empty slug or without an
// input, an unknown type, two sections with one slug, an input or a toc path
// outside the root, a docs input that names no directory reached through no
// symlink, a toc on a spec section, a link section that linkSection refuses,
// and a URL on a section of another type.
func openSections(root fs.FS, sections []Section) ([]*section, error) {
	if root == nil {
		return nil, errors.New("no documentation root")
	}
	if len(sections) == 0 {
		return nil, errors.New("no sections")
	}
	opened := make([]*section, 0, len(sections))
	titles := map[string]string{} // the title of the section with each slug
	for i, s := range sections {
		if s.Title == "" {
			return nil, fmt.Errorf("section %d has no title", i+1)
		}
		sec, err := openSection(root, s)
		if err != nil {
			return nil, fmt.Errorf("section %q: %w", s.Title, err)
		}
		if other, taken := titles[sec.slug]; taken {
			return nil, fmt.Errorf("sections %q and %q share the slug %q", other, s.Title, sec.slug)
		}
		titles[sec.slug] = s.Title
		opened = append(opened, sec)
	}
	return opened, nil
}

// openSection checks s, a section with a title, with its paths cleaned, and
// opens its content directory when it is a docs section. A link section has
// no paths, and linkSection checks it.
func openSection(root fs.FS, s Section) (*section, error) {
	sec := &section{Section: s, config: s, slug: slugOf(s.Title)}
	sec.Input, sec.Toc = cleanPath(s.Input), cleanPath(s.Toc)
	switch {
	case sec.slug == "":
		return nil, errors.New("the title gives an empty slug")
	case s.Type == LinkSection:
		return linkSection(sec)
	case s.URL != "":
		return nil, fmt.Errorf("url %q needs the type %q", s.URL, LinkSection)
	case sec.Input == "":
		return nil, fmt.Errorf("input %q names no path inside the documentation root", s.Input)
	case s.Toc != "" && (sec.Toc == "" || sec.Toc == "." || !fs.ValidPath(sec.Toc)):
		return nil, fmt.Errorf("toc path %q is not a file inside the documentation root", s.Toc)
	}
	switch s.Type {
	case SpecSection:
		if sec.Toc != "" {
			return nil, fmt.Errorf("toc path %q needs a content directory, whose document pages show the sidebar", sec.Toc)
		}
		if sec.Input == "." || !fs.ValidPath(sec.Input) {
			return nil, fmt.Errorf("spec path %q is not a file inside the documentation root", sec.Input)
		}
	case DocsSection:
		docs, err := openDocs(root, sec.Input)
		if err != nil {
			return nil, err
		}
		sec.docs = docs
	default:
		return nil, fmt.Errorf("the type %q is none of %q, %q and %q", s.Type, SpecSection, DocsSection, LinkSection)
	}
	return sec, nil
}

// linkSection returns sec, a link section with a slug, with the URL that its
// navigation bar links, or refuses an input, a toc and a URL that linkURL
// refuses.
func linkSection(sec *section) (*section, error) {
	switch {
	case sec.URL == "":
		return nil, errors.New("a link section needs a url")
	case sec.config.Input != "":
		return nil, fmt.Errorf("input %q: a link section takes a url and no input", sec.config.Input)
	case sec.config.Toc != "":
		return nil, fmt.Errorf("toc path %q needs a content directory, whose document pages show the sidebar", sec.config.Toc)
	}
	link, err := linkURL(sec.URL)
	if err != nil {
		return nil, err
	}
	sec.link = link
	return sec, nil
}

// linkURL returns raw, a link section's URL, as net/url writes it back, or
// refuses a URL that is neither a path starting with one slash nor an http
// or https URL with a host.
func linkURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("url %q: %w", raw, err)
	}
	// String would write a path of two slashes back as a host.
	p := u.EscapedPath()
	rooted := u.Scheme == "" && u.Host == "" && u.User == nil && strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
	web := (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	if !rooted && !web {
		return "", fmt.Errorf("url %q is neither a path that starts with one / nor an http or https URL with a host", raw)
	}
	return u.String(), nil
}

// cleanPath returns p without a leading ./ and a trailing /, so that ./docs/
// names docs.
func cleanPath(p string) string {
	return strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
}

// openDocs opens the content directory at p in root, or refuses a path
// outside root and one that names no directory reached through no symlink.
func openDocs(root fs.FS, p string) (fs.FS, error) {
	docs, err := fs.Sub(root, p)
	var info fs.FileInfo
	if err == nil {
		info, err = fs.Stat(docs, ".")
	}
	if err != nil {
		return nil, fmt.Errorf("content directory path %q: %w", p, err)
	}
	if !info.IsDir() || !directory(root, p) {
		return nil, fmt.Errorf("content directory path %q is not a directory reached through no symlink", p)
	}
	return docs, nil
}

// slugOf returns the section slug of title: title in lower case, with each
// run of characters other than letters and digits as one dash, and no dash
// at either end.
func slugOf(title string) string {
	var slug strings.Builder
	dash := false // whether a run of other characters precedes the next letter or digit
	for _, r := range strings.ToLower(title) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			dash = true
			continue
		}
		if dash && slug.Len() > 0 {
			slug.WriteByte('-')
		}
		dash = false
		slug.WriteRune(r)
	}
	return slug.String()
}
