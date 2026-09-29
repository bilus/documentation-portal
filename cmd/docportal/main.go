// Command docportal serves the documentation of an API spec from a local
// directory.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

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
	// HOLE(1): parse -addr, -specs-dir and -spec-path, falling back to DOCPORTAL_ADDR, DOCPORTAL_SPECS_DIR and DOCPORTAL_SPEC_PATH, then to :8080, . and openapi.yaml; reject unknown flags and positional arguments
	return config{Addr: ":8080", SpecsDir: "../../testdata/specs", SpecPath: "petstore-3.0.yaml"}, nil
}

// openSpecs opens the specs directory and pairs it with the spec path, so
// that the portal cannot read outside the directory.
func openSpecs(dir, specPath string) (portal.Config, error) {
	// HOLE(1): open dir with os.OpenRoot and return its FS with specPath; fail with an error naming dir when it cannot be opened
	return portal.Config{Specs: os.DirFS(dir), SpecPath: specPath}, nil
}
