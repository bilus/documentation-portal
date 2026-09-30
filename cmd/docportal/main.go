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
	RootDir   string
	SpecPath  string // relative to RootDir
	DocsPath  string // relative to RootDir, empty without a content directory
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

// startup reads the configuration, opens the documentation root, adds the Try
// It setting, and builds the portal. It returns the address to listen on and
// the portal.
func startup(args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	pcfg, err := openRoot(cfg.RootDir, cfg.SpecPath, cfg.DocsPath)
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
	cfg := config{Addr: ":8080", RootDir: ".", SpecPath: "openapi.yaml"}
	if v := getenv("DOCPORTAL_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("DOCPORTAL_ROOT_DIR"); v != "" {
		cfg.RootDir = v
	}
	if v := getenv("DOCPORTAL_SPEC_PATH"); v != "" {
		cfg.SpecPath = v
	}
	if v := getenv("DOCPORTAL_DOCS_PATH"); v != "" {
		cfg.DocsPath = v
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
	fs.StringVar(&cfg.RootDir, "root-dir", cfg.RootDir, "documentation root, the directory that holds the API spec and the markdown files")
	fs.StringVar(&cfg.SpecPath, "spec-path", cfg.SpecPath, "API spec to serve, relative to -root-dir")
	fs.StringVar(&cfg.DocsPath, "docs-path", cfg.DocsPath, "directory of the markdown files and their images, relative to -root-dir")
	fs.BoolVar(&cfg.HideTryIt, "hide-try-it", cfg.HideTryIt, "hide the Try It console of the viewer page")
	if err := fs.Parse(args); err != nil {
		return config{}, errors.New(strings.TrimSpace(out.String()))
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}

// openRoot opens the documentation root and pairs it with the spec path and
// the content directory path, so that the portal cannot read outside the root.
func openRoot(dir, specPath, docsPath string) (portal.Config, error) {
	// The root stays open for as long as docportal runs.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return portal.Config{}, fmt.Errorf("open documentation root: %w", err)
	}
	return portal.Config{Root: root.FS(), SpecPath: specPath, DocsPath: docsPath}, nil
}

// setTryIt adds the Try It setting to the portal configuration.
func setTryIt(pcfg portal.Config, hide bool) portal.Config {
	pcfg.HideTryIt = hide
	return pcfg
}
