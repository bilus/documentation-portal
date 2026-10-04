// Command docportal serves the documentation of API specs and markdown files
// from a local directory or a bucket folder, as the portals of a
// configuration file, each a set of sections.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// The drivers of the bucket folder URLs that -root accepts.
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"

	"github.com/bilus/documentation-portal/anthropicmodel"
	"github.com/bilus/documentation-portal/chat"
	"github.com/bilus/documentation-portal/portal"
	"github.com/bilus/documentation-portal/source"
)

type config struct {
	Addr       string
	ConfigName string        // the configuration file: a local path, or with Root a path inside the bucket folder
	Root       string        // the bucket folder's URL, or empty for the directory of ConfigName
	Refresh    time.Duration // between checks of the bucket folder; 0 turns them off
	MaxSize    int64         // of a bucket folder's objects, in bytes
	HideTryIt  bool
	ChatModel  string // empty without a chat
}

func main() {
	addr, h, err := startup(context.Background(), os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docportal:", err)
		os.Exit(1)
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

// startup reads the configuration, opens the documentation source, loads its
// snapshot, builds the portal handler from it, and wraps it in the reloader,
// which rebuilds it for each settled change of the source until ctx ends.
// It returns the address to listen on and the reloader.
func startup(ctx context.Context, args []string, getenv func(string) string) (string, http.Handler, error) {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return "", nil, err
	}
	src, configPath, err := openSource(ctx, cfg)
	if err != nil {
		return "", nil, err
	}
	snap, err := source.Load(ctx, src)
	if err != nil {
		return "", nil, err
	}
	b := &builder{cfg: cfg, configPath: configPath}
	h, err := b.build(snap.Root)
	if err != nil {
		return "", nil, err
	}
	return cfg.Addr, source.NewReloader(ctx, src, snap.Listing, h, cfg.Refresh, b.build), nil
}

// builder builds the portal handler of each snapshot of the documentation
// source, with one chat across the snapshots.
type builder struct {
	cfg        config
	configPath string     // the configuration file's path inside the snapshot
	chat       *chat.Chat // nil until the first build with a chat model
}

// build builds the portal handler of the snapshot at root: it reads the
// portal configuration from the configuration file, adds the Try It setting
// and the chat, and builds the handler, as startup's boxes do for the first
// snapshot. A build that fails leaves the chat as it was.
func (b *builder) build(root fs.FS) (http.Handler, error) {
	pcfg, err := portal.ReadConfig(root, b.configPath)
	if err != nil {
		return nil, err
	}
	pcfg = setTryIt(pcfg, b.cfg.HideTryIt)
	pcfg, b.chat, err = addChat(pcfg, b.cfg.ChatModel, b.chat)
	if err != nil {
		return nil, err
	}
	return portal.New(pcfg)
}

// parseConfig reads the configuration from the flags and the environment.
// Flags win over the environment, which wins over the defaults.
func parseConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{Addr: ":8080", ConfigName: "environment.yaml", Refresh: time.Minute, MaxSize: 256 << 20}
	if v := getenv("DOCPORTAL_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("DOCPORTAL_CONFIG"); v != "" {
		cfg.ConfigName = v
	}
	if v := getenv("DOCPORTAL_ROOT"); v != "" {
		cfg.Root = v
	}
	if v := getenv("DOCPORTAL_REFRESH"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_REFRESH: %q is not a duration", v)
		}
		cfg.Refresh = d
	}
	maxSizeMiB := cfg.MaxSize >> 20
	if v := getenv("DOCPORTAL_MAX_SIZE"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return config{}, fmt.Errorf("DOCPORTAL_MAX_SIZE: %q is not a number of MiB", v)
		}
		maxSizeMiB = n
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
	flags.StringVar(&cfg.ConfigName, "config", cfg.ConfigName, "configuration file, which lists the portals and their sections; its directory is the documentation root, or with -root its path inside the bucket folder")
	flags.StringVar(&cfg.Root, "root", cfg.Root, "bucket folder that holds the documentation, as a Go CDK URL such as gs://bucket?prefix=docs/; without it, the directory of -config")
	flags.DurationVar(&cfg.Refresh, "refresh", cfg.Refresh, "how often to check the bucket folder for changes; 0 never")
	flags.Int64Var(&maxSizeMiB, "max-size", maxSizeMiB, "largest total size of the bucket folder's objects, in MiB; 0 means no limit")
	flags.BoolVar(&cfg.HideTryIt, "hide-try-it", cfg.HideTryIt, "hide the Try It console of the viewer page")
	flags.StringVar(&cfg.ChatModel, "chat-model", cfg.ChatModel, "Anthropic model of the chat page, such as claude-opus-5-5; none disables the chat")
	if err := flags.Parse(args); err != nil {
		return config{}, errors.New(strings.TrimSpace(out.String()))
	}
	if flags.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if maxSizeMiB < 0 || maxSizeMiB > math.MaxInt64>>20 {
		return config{}, fmt.Errorf("-max-size: %d MiB is not a size between 0 and %d", maxSizeMiB, math.MaxInt64>>20)
	}
	cfg.MaxSize = maxSizeMiB << 20
	return cfg, nil
}

// openSource opens the documentation source of cfg: the directory of the
// configuration file, so that the portal cannot read outside it, or the
// bucket folder that cfg.Root names, with cfg's size limit. It returns the
// source with the configuration file's path inside it. A file:// bucket
// folder, unlike the directory, reads through a symlink to a file outside
// it, as the file driver does.
func openSource(ctx context.Context, cfg config) (source.Source, string, error) {
	if cfg.Root != "" {
		bucket, err := source.OpenBucket(ctx, cfg.Root, cfg.MaxSize)
		if err != nil {
			return nil, "", fmt.Errorf("open bucket folder %s: %w", cfg.Root, err)
		}
		return bucket, cfg.ConfigName, nil
	}
	// The root stays open for as long as docportal runs.
	dir, err := source.OpenDirectory(filepath.Dir(cfg.ConfigName))
	if err != nil {
		return nil, "", fmt.Errorf("open documentation root: %w", err)
	}
	return dir, filepath.Base(cfg.ConfigName), nil
}

// setTryIt adds the Try It setting to the portal configuration.
func setTryIt(pcfg portal.Config, hide bool) portal.Config {
	pcfg.HideTryIt = hide
	return pcfg
}

// addChat adds the chat's routes to the portal configuration when modelID
// names a model, so that the portal handler serves a chat page in every
// portal. It builds the chat for the first snapshot, when c is nil, and gives
// an existing chat the new snapshot's libraries, so that the chat keeps its
// conversations across snapshots. It returns the chat for the next snapshot:
// the one it built or was given, and on a failure the one it was given, so
// that a snapshot that fails never costs the chat its conversations.
func addChat(pcfg portal.Config, modelID string, c *chat.Chat) (portal.Config, *chat.Chat, error) {
	if modelID == "" {
		return pcfg, nil, nil
	}
	libs, err := portal.NewLibraries(pcfg)
	if err != nil {
		return portal.Config{}, c, err
	}
	if c == nil {
		m, err := anthropicmodel.New(modelID, anthropicmodel.Config{})
		if err != nil {
			return portal.Config{}, nil, err
		}
		if c, err = chat.New(chat.Config{Model: m, Libraries: libs}); err != nil {
			return portal.Config{}, nil, err
		}
	} else if err := c.Reload(libs); err != nil {
		return portal.Config{}, c, err
	}
	pcfg.Chat = c.Routes()
	return pcfg, c, nil
}
