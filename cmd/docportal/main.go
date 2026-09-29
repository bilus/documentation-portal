// Command docportal serves the documentation of an API spec from a local
// directory.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/bilus/documentation-portal/portal"
)

type config struct {
	Addr      string
	SpecsDir  string
	SpecPath  string // relative to SpecsDir
	DocsDir   string // empty without a content directory
	HideTryIt bool
}

func main() {
	addr, h, err := startup(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docportal:", err)
		os.Exit(1)
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

// startup reads the configuration, opens the specs directory and the content
// directory, adds the Try It setting, and builds the portal. It returns the
// address to listen on and the portal.
func startup(args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	pcfg, err := openSpecs(cfg.SpecsDir, cfg.SpecPath)
	if err != nil {
		return "", nil, err
	}
	pcfg, err = openDocs(pcfg, cfg.DocsDir)
	if err != nil {
		return "", nil, err
	}
	pcfg = setTryIt(pcfg, cfg.HideTryIt)
	h, err := portal.New(pcfg)
	if err != nil {
		return "", nil, err
	}
	return cfg.Addr, h, nil
}

// parseConfig reads the configuration from the flags and the environment.
// Flags win over the environment, which wins over the defaults.
func parseConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{Addr: ":8080", SpecsDir: ".", SpecPath: "openapi.yaml"}
	if v := getenv("DOCPORTAL_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("DOCPORTAL_SPECS_DIR"); v != "" {
		cfg.SpecsDir = v
	}
	if v := getenv("DOCPORTAL_SPEC_PATH"); v != "" {
		cfg.SpecPath = v
	}
	if v := getenv("DOCPORTAL_DOCS_DIR"); v != "" {
		cfg.DocsDir = v
	}
	if v := getenv("DOCPORTAL_HIDE_TRY_IT"); v != "" {
		hide, err := strconv.ParseBool(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_HIDE_TRY_IT: %q is not a boolean", v)
		}
		cfg.HideTryIt = hide
	}

	// The flag package's message and usage go into the error, which main prints.
	var out strings.Builder
	fs := flag.NewFlagSet("docportal", flag.ContinueOnError)
	fs.SetOutput(&out)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "address to listen on")
	fs.StringVar(&cfg.SpecsDir, "specs-dir", cfg.SpecsDir, "directory that holds the API specs")
	fs.StringVar(&cfg.SpecPath, "spec-path", cfg.SpecPath, "API spec to serve, relative to -specs-dir")
	fs.StringVar(&cfg.DocsDir, "docs-dir", cfg.DocsDir, "directory that holds the markdown files and their images")
	fs.BoolVar(&cfg.HideTryIt, "hide-try-it", cfg.HideTryIt, "hide the Try It console of the viewer page")
	if err := fs.Parse(args); err != nil {
		return config{}, errors.New(strings.TrimSpace(out.String()))
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}

// openSpecs opens the specs directory and pairs it with the spec path, so
// that the portal cannot read outside the directory.
func openSpecs(dir, specPath string) (portal.Config, error) {
	// The root stays open for as long as docportal runs.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return portal.Config{}, fmt.Errorf("open specs directory: %w", err)
	}
	return portal.Config{Specs: root.FS(), SpecPath: specPath}, nil
}

// openDocs opens the named content directory, if any, and adds it to the
// portal configuration. Without a content directory name it returns pcfg
// unchanged.
func openDocs(pcfg portal.Config, dir string) (portal.Config, error) {
	if dir == "" {
		return pcfg, nil
	}
	// The root stays open for as long as docportal runs.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return portal.Config{}, fmt.Errorf("open content directory: %w", err)
	}
	pcfg.Docs = root.FS()
	return pcfg, nil
}

// setTryIt adds the Try It setting to the portal configuration.
func setTryIt(pcfg portal.Config, hide bool) portal.Config {
	// HOLE(1): set pcfg.HideTryIt to hide
	return pcfg
}
