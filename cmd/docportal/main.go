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
	"strings"

	"github.com/bilus/documentation-portal/portal"
)

type config struct {
	Addr     string
	SpecsDir string
	SpecPath string // relative to SpecsDir
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

// startup reads the configuration, opens the specs directory and builds the
// portal. It returns the address to listen on and the portal.
func startup(args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	pcfg, err := openSpecs(cfg.SpecsDir, cfg.SpecPath)
	if err != nil {
		return "", nil, err
	}
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

	// The flag package's message and usage go into the error, which main prints.
	var out strings.Builder
	fs := flag.NewFlagSet("docportal", flag.ContinueOnError)
	fs.SetOutput(&out)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "address to listen on")
	fs.StringVar(&cfg.SpecsDir, "specs-dir", cfg.SpecsDir, "directory that holds the API specs")
	fs.StringVar(&cfg.SpecPath, "spec-path", cfg.SpecPath, "API spec to serve, relative to -specs-dir")
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
