package portal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"gopkg.in/yaml.v3"
)

// Section is a spec or a content directory of the documentation root, shown
// under its title in the navigation bar.
type Section struct {
	Title string      `yaml:"title"`
	Type  SectionType `yaml:"type"`
	Input string      `yaml:"input"` // the spec path or the content directory path, relative to Root
	Toc   string      `yaml:"toc"`   // a docs section's toc path, relative to Root, or empty
}

// SectionType says what a section's input names.
type SectionType string

const (
	SpecSection SectionType = "spec" // an API spec
	DocsSection SectionType = "docs" // a content directory
)

// ReadConfig reads the sections from the configuration file at name, a path
// inside root, into the portal configuration of root. It refuses a file that
// is missing or not one YAML document, and a key it does not know. New checks
// the sections.
func ReadConfig(root fs.FS, name string) (Config, error) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration file: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var file struct {
		Sections []Section `yaml:"sections"`
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
	return Config{Root: root, Sections: file.Sections}, nil
}

// section is a section of the portal configuration after openSections has
// checked it, with its slug and, for a docs section, its content directory.
type section struct {
	Section
	slug string
	docs fs.FS // a docs section's content directory handle, else nil
}

// openSections checks the sections of cfg and opens the content directory of
// each docs section, so that every input and toc path stays inside the
// documentation root. It cleans a leading ./ and a trailing / from each path,
// and refuses no documentation root, no sections, a section without a title,
// with an empty slug or without an input, an unknown type, two sections with
// one slug, an input or a toc path outside the root, a docs input that names
// no directory reached through no symlink, and a toc on a spec section.
func openSections(cfg Config) ([]*section, error) {
	// HOLE(1): check every section and open each content directory. Until
	// then, the checks of the old checkConfig apply to the first spec section
	// and the first docs section, and a toc on a spec section stands for a toc
	// path without a content directory path.
	if cfg.Root == nil {
		return nil, errors.New("no documentation root")
	}
	var sections []*section
	var spec, docs *section
	for _, s := range cfg.Sections {
		sec := &section{Section: s, slug: slugOf(s.Title)}
		switch {
		case s.Type == SpecSection && spec == nil:
			spec = sec
		case s.Type == DocsSection && docs == nil:
			docs = sec
		}
		sections = append(sections, sec)
	}
	var specPath, docsPath, tocPath string
	if spec != nil {
		specPath, tocPath = spec.Input, spec.Toc
	}
	if docs != nil {
		docsPath, tocPath = docs.Input, docs.Toc
	}
	if specPath == "." || !fs.ValidPath(specPath) {
		return nil, fmt.Errorf("spec path %q is not a file inside the documentation root", specPath)
	}
	if tocPath != "" && (tocPath == "." || !fs.ValidPath(tocPath)) {
		return nil, fmt.Errorf("toc path %q is not a file inside the documentation root", tocPath)
	}
	if tocPath != "" && docsPath == "" {
		return nil, fmt.Errorf("toc path %q needs a content directory path, whose document pages show the sidebar", tocPath)
	}
	if docs == nil {
		return sections, nil
	}
	sub, err := fs.Sub(cfg.Root, docsPath)
	var info fs.FileInfo
	if err == nil {
		info, err = fs.Stat(sub, ".")
	}
	if err != nil {
		return nil, fmt.Errorf("content directory path %q: %w", docsPath, err)
	}
	if !info.IsDir() || !directory(cfg.Root, docsPath) {
		return nil, fmt.Errorf("content directory path %q is not a directory reached through no symlink", docsPath)
	}
	docs.docs = sub
	return sections, nil
}

// slugOf returns the section slug of title: title in lower case, with each
// run of characters other than letters and digits as one dash, and no dash
// at either end.
func slugOf(title string) string {
	// HOLE(1): make the slug
	return title
}
