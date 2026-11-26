package portal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unsafe"

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
// /portals/{slug}/, where the slug comes from the name, or with the type
// LinkPortal a link to a page outside the portal handler, shown under its
// name on the home page and in the portal menu.
type Portal struct {
	Name     string         `yaml:"name"`
	Type     PortalType     `yaml:"type"` // empty for a portal with sections, or LinkPortal
	URL      string         `yaml:"url"`  // a link portal's URL: a path from the server's root, or an http or https URL
	Sections []Section      `yaml:"sections"`
	Labels   []string       `yaml:"labels"`  // for the access hook's rules and the collections' match labels
	Ext      map[string]any `yaml:",inline"` // the extension fields: those whose keys start with x-, by key, or nil
}

// Clone returns a copy of the portal that shares nothing with it: its own
// labels, its own sections with their labels, and its own extension fields,
// at every depth and whatever the types of their values. The portal handler keeps a clone of each portal of its
// configuration, and gives each caller, such as the access hook, a clone of
// its own, so that no caller's write reaches another.
func (p Portal) Clone() Portal {
	p.Labels = slices.Clone(p.Labels)
	if p.Sections != nil {
		sections := make([]Section, len(p.Sections))
		for i, s := range p.Sections {
			sections[i] = s.Clone()
		}
		p.Sections = sections
	}
	if p.Ext != nil {
		p.Ext = cloneValue(p.Ext).(map[string]any)
	}
	return p
}

// Clone returns a copy of the section with labels of its own.
func (s Section) Clone() Section {
	s.Labels = slices.Clone(s.Labels)
	return s
}

// cloneValue returns a copy of v, a value of an extension field, that shares
// no map, list, array or pointer with it, whatever their types: those that
// YAML gives, such as map[string]any, and those set in code, such as
// []string or a pointer to a struct. A channel and a function, which have no
// copy, stay shared.
func cloneValue(v any) any {
	if v == nil {
		return nil
	}
	return deepCopy(reflect.ValueOf(v), map[visit]reflect.Value{}).Interface()
}

// visit names a map, a list or a pointer that deepCopy has copied, so that a
// value that holds itself is copied once.
type visit struct {
	typ reflect.Type
	ptr unsafe.Pointer
	len int
}

// deepCopy returns a copy of v that shares no map, list, array or pointer
// with it, and with seen keeps each one's copy for its other holders.
func deepCopy(v reflect.Value, seen map[visit]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		key := visit{v.Type(), v.UnsafePointer(), 0}
		if c, ok := seen[key]; ok {
			return c
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		seen[key] = c
		for iter := v.MapRange(); iter.Next(); {
			c.SetMapIndex(deepCopy(iter.Key(), seen), deepCopy(iter.Value(), seen))
		}
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		key := visit{v.Type(), v.UnsafePointer(), v.Len()}
		if c, ok := seen[key]; ok {
			return c
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		seen[key] = c
		for i := range v.Len() {
			c.Index(i).Set(deepCopy(v.Index(i), seen))
		}
		return c
	case reflect.Array:
		c := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			c.Index(i).Set(deepCopy(v.Index(i), seen))
		}
		return c
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		key := visit{v.Type(), v.UnsafePointer(), 0}
		if c, ok := seen[key]; ok {
			return c
		}
		c := reflect.New(v.Type().Elem())
		seen[key] = c
		c.Elem().Set(deepCopy(v.Elem(), seen))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(deepCopy(v.Elem(), seen))
		return c
	case reflect.Struct:
		// A field that is not exported, as in time.Time, keeps its value.
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		for i := range v.NumField() {
			if c.Field(i).CanSet() {
				c.Field(i).Set(deepCopy(v.Field(i), seen))
			}
		}
		return c
	}
	return v
}

// GetExt returns the portal's extension fields as a T, whose fields name
// their keys in yaml tags, such as `yaml:"x-host"`: T's zero value without
// extension fields, or an error for an extension field that T lacks and for a
// value that does not fit T's field. As in YAML, a number or a boolean fits a
// string field, and a null leaves its field at the zero value. The result
// shares nothing with the portal.
func (p Portal) GetExt[T any]() (T, error) {
	return decodeExt[T](p.Ext, fmt.Sprintf("portal %q", p.Name))
}

// decodeExt returns fields, the extension fields of owner, a portal or a
// collection, as a T: T's zero value without fields, or an error that names
// owner for a field that T lacks, for a value that does not fit T's field
// and for a value that YAML cannot write.
func decodeExt[T any](fields map[string]any, owner string) (ext T, err error) {
	if len(fields) == 0 {
		return ext, nil
	}
	// The YAML encoder panics on a value it cannot write, such as a channel
	// set in code.
	defer func() {
		if r := recover(); r != nil {
			var none T
			ext, err = none, fmt.Errorf("%s: extension fields: %v", owner, r)
		}
	}()
	data, err := yaml.Marshal(fields)
	if err != nil {
		return ext, fmt.Errorf("%s: extension fields: %w", owner, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&ext); err != nil {
		var none T
		return none, fmt.Errorf("%s: extension fields: %w", owner, err)
	}
	return ext, nil
}

// extensionFields returns unknown, the keys of a portal or of a collection
// that its fields do not name, as its extension fields, or nil without any. It refuses, naming
// every one in order, the keys that do not start with x- and the key x-
// alone, and then the first field in order whose value extensionValue
// refuses.
func extensionFields(unknown map[string]any) (map[string]any, error) {
	if len(unknown) == 0 {
		return nil, nil
	}
	keys := slices.Sorted(maps.Keys(unknown))
	var refused []string
	for _, key := range keys {
		if !strings.HasPrefix(key, "x-") || key == "x-" {
			refused = append(refused, strconv.Quote(key))
		}
	}
	switch len(refused) {
	case 0:
	case 1:
		return nil, fmt.Errorf("unknown key %s: only a key that starts with x- and names a field is free", refused[0])
	default:
		return nil, fmt.Errorf("unknown keys %s: only a key that starts with x- and names a field is free", strings.Join(refused, ", "))
	}
	for _, key := range keys {
		if err := extensionValue(unknown[key]); err != nil {
			return nil, fmt.Errorf("extension field %s: %w", key, err)
		}
	}
	return unknown, nil
}

// extensionValue refuses v, a YAML value, when it holds, at any depth, a map
// with a key that is not a string, or a number that is not finite, which
// JSON cannot write.
func extensionValue(v any) error {
	switch v := v.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(v)) {
			if err := extensionValue(v[key]); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range v {
			if err := extensionValue(e); err != nil {
				return err
			}
		}
	case map[any]any:
		return errors.New("a map key that is not a string")
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("a number that is not finite, such as .nan or .inf")
		}
	}
	return nil
}

// PortalType says whether a portal holds sections or links a page.
type PortalType string

// LinkPortal is the type of a link portal, which has a URL and no sections.
const LinkPortal PortalType = "link"

// Collection is a named set of portals, listed under its name on the home page
// and in the portal menu: the portals whose labels hold every match label.
type Collection struct {
	Name        string         `yaml:"name"`
	MatchLabels []string       `yaml:"matchLabels"`
	Ext         map[string]any `yaml:",inline"` // the extension fields: those whose keys start with x-, by key, or nil
}

// Clone returns a copy of the collection that shares nothing with it: its own
// match labels and its own extension fields, at every depth and whatever the
// types of their values. The portal handler keeps a clone of each collection
// of its configuration, and gives each call of a collection access a clone of
// its own.
func (c Collection) Clone() Collection {
	c.MatchLabels = slices.Clone(c.MatchLabels)
	if c.Ext != nil {
		c.Ext = cloneValue(c.Ext).(map[string]any)
	}
	return c
}

// GetExt returns the collection's extension fields as a T, whose fields name
// their keys in yaml tags, such as `yaml:"x-roles"`, as Portal.GetExt does
// for a portal: T's zero value without extension fields, or an error that
// names the collection for an extension field that T lacks and for a value
// that does not fit T's field. The result shares nothing with the collection.
func (c Collection) GetExt[T any]() (T, error) {
	return decodeExt[T](c.Ext, fmt.Sprintf("collection %q", c.Name))
}

// ReadConfig reads the portals and the collections from the configuration
// file at name, a path inside root, into the portal configuration of root. It
// keeps the fields of a portal and of a collection whose keys start with x-
// as its extension fields.
// It refuses no root, a file that is missing, that is not YAML or that holds
// more than one YAML document, any other key it does not know, and an empty
// entry in a list of portals, of sections, of collections or of match
// labels. An empty file gives no portals. New checks the portals and the
// collections.
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
			Type     PortalType `yaml:"type"`
			URL      string     `yaml:"url"`
			Sections []*Section `yaml:"sections"`
			// Every key that the fields above do not name, of which only x- keys pass.
			Ext    map[string]any `yaml:",inline"`
			Labels []string       `yaml:"labels"`
		} `yaml:"portals"`
		// Pointers keep a null match label, which yaml.v3 drops from a list of strings.
		Collections []*struct {
			Name        string    `yaml:"name"`
			MatchLabels []*string `yaml:"matchLabels"`
			// Every key that the fields above do not name, of which only x- keys pass.
			Ext map[string]any `yaml:",inline"`
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
		ext, err := extensionFields(p.Ext)
		if err != nil {
			return Config{}, fmt.Errorf("configuration file %s: portal %q: %w", name, p.Name, err)
		}
		portals = append(portals, Portal{Name: p.Name, Type: p.Type, URL: p.URL, Sections: sections, Labels: p.Labels, Ext: ext})
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
		ext, err := extensionFields(g.Ext)
		if err != nil {
			return Config{}, fmt.Errorf("configuration file %s: collection %q: %w", name, g.Name, err)
		}
		collections = append(collections, Collection{Name: g.Name, MatchLabels: labels, Ext: ext})
	}
	return Config{Root: root, Portals: portals, Collections: collections}, nil
}

// openPortals checks the portals of cfg and their sections and opens the
// content directory of each docs section, so that every input and toc path
// stays inside the documentation root. It returns a site for each portal, in
// order, and refuses no portals, a portal without a name, with an empty slug
// or with the slug of another portal, a type other than LinkPortal, a URL
// without that type, a link portal that linkPortal refuses, and the sections
// that openSections refuses.
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
		// The sites keep copies, so that a later write to cfg reaches no page.
		p = p.Clone()
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
		if p.Type == LinkPortal {
			s, err := linkPortal(cfg, p)
			if err != nil {
				return nil, fmt.Errorf("portal %q: %w", p.Name, err)
			}
			sites = append(sites, s)
			continue
		}
		switch {
		case p.Type != "":
			return nil, fmt.Errorf("portal %q: the type %q is not %q", p.Name, p.Type, LinkPortal)
		case p.URL != "":
			return nil, fmt.Errorf("portal %q: url %q needs the type %q", p.Name, shownURL(p.URL), LinkPortal)
		}
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

// linkPortal returns the site of p, a link portal of cfg with a name and a
// slug, with the URL that the home page and the portal menu link, or refuses
// sections and a URL that linkURL refuses.
func linkPortal(cfg Config, p Portal) (*site, error) {
	switch {
	case p.URL == "":
		return nil, errors.New("a link portal needs a url")
	case len(p.Sections) > 0:
		return nil, errors.New("a link portal takes a url and no sections")
	}
	link, err := linkURL(p.URL)
	if err != nil {
		return nil, err
	}
	s := newSite(cfg, p, nil)
	s.link = link
	return s, nil
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
		return nil, fmt.Errorf("url %q needs the type %q", shownURL(s.URL), LinkSection)
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
// or https URL with a host, and one with a user name or a password.
func linkURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// The parser's error quotes the whole URL, so only its cause is kept,
		// and not even that beside a user part.
		var parse *url.Error
		if strings.Contains(raw, "@") || !errors.As(err, &parse) {
			return "", fmt.Errorf("url %q does not parse", shownURL(raw))
		}
		return "", fmt.Errorf("url %q: %w", raw, parse.Err)
	}
	if u.User != nil {
		// The error leaves the user part out, which may hold a password.
		u.User = nil
		return "", fmt.Errorf("url %q holds a user name or a password", u.String())
	}
	// String would write a path of two slashes back as a host.
	p := u.EscapedPath()
	rooted := u.Scheme == "" && u.Host == "" && strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
	web := (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	if !rooted && !web {
		return "", fmt.Errorf("url %q is neither a path that starts with one / nor an http or https URL with a host", shownURL(raw))
	}
	return u.String(), nil
}

// shownURL returns raw, a URL of the configuration, for an error message:
// as it is without an @, which every user part needs, else without its user
// part, and not at all when it does not parse, so that no message holds a
// password.
func shownURL(raw string) string {
	if !strings.Contains(raw, "@") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(not shown)"
	}
	u.User = nil
	return u.String()
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
