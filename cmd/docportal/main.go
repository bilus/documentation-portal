// Command docportal serves the documentation of API specs and markdown files
// from a local directory, as the sections of a configuration file.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
)

type config struct {
	Addr       string
	ConfigName string // the configuration file, whose directory is the documentation root
	HideTryIt  bool
	ChatModel  string // empty without a chat
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

// startup reads the configuration, opens the documentation root, reads the
// portal configuration from the configuration file, adds the Try It setting
// and the chat, and builds the portal. It returns the address to listen on and
// the portal.
func startup(args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	root, name, err := openRoot(cfg.ConfigName)
	if err != nil {
		return "", nil, err
	}
	pcfg, err := portal.ReadConfig(root, name)
	if err != nil {
		return "", nil, err
	}
	pcfg = setTryIt(pcfg, cfg.HideTryIt)
	pcfg, err = addChat(pcfg, cfg.ChatModel)
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
	cfg := config{Addr: ":8080", ConfigName: "environment.yaml"}
	if v := getenv("DOCPORTAL_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("DOCPORTAL_CONFIG"); v != "" {
		cfg.ConfigName = v
	}
	if v := getenv("DOCPORTAL_CHAT_MODEL"); v != "" {
		cfg.ChatModel = v
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
	flags := flag.NewFlagSet("docportal", flag.ContinueOnError)
	flags.SetOutput(&out)
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "address to listen on")
	flags.StringVar(&cfg.ConfigName, "config", cfg.ConfigName, "configuration file, which lists the sections; its directory is the documentation root")
	flags.BoolVar(&cfg.HideTryIt, "hide-try-it", cfg.HideTryIt, "hide the Try It console of the viewer page")
	flags.StringVar(&cfg.ChatModel, "chat-model", cfg.ChatModel, "Anthropic model of the chat page, such as claude-opus-5-5; none disables the chat")
	if err := flags.Parse(args); err != nil {
		return config{}, errors.New(strings.TrimSpace(out.String()))
	}
	if flags.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	return cfg, nil
}

// openRoot opens the directory of the configuration file named configName as
// the documentation root, so that the portal cannot read outside it, and
// returns it with the configuration file's path inside it.
func openRoot(configName string) (fs.FS, string, error) {
	// The root stays open for as long as docportal runs.
	root, err := os.OpenRoot(filepath.Dir(configName))
	if err != nil {
		return nil, "", fmt.Errorf("open documentation root: %w", err)
	}
	return root.FS(), filepath.Base(configName), nil
}

// setTryIt adds the Try It setting to the portal configuration.
func setTryIt(pcfg portal.Config, hide bool) portal.Config {
	pcfg.HideTryIt = hide
	return pcfg
}

// addChat adds the chat's routes to the portal configuration when modelID
// names a model, so that the portal serves the chat page.
func addChat(pcfg portal.Config, modelID string) (portal.Config, error) {
	if modelID == "" {
		return pcfg, nil
	}
	lib, err := portal.NewLibrary(pcfg)
	if err != nil {
		return portal.Config{}, err
	}
	m, err := anthropicmodel.New(modelID, anthropicmodel.Config{})
	if err != nil {
		return portal.Config{}, err
	}
	c, err := chat.New(chat.Config{Model: m, Library: lib})
	if err != nil {
		return portal.Config{}, err
	}
	pcfg.Chat = c.Routes()
	return pcfg, nil
}
