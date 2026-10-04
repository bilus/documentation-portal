// Command portalapp is an example application that embeds the portal
// library: it serves the documentation of a bucket folder with previews,
// signs readers in through Auth0 or GitHub, and opens portals, sections and
// previews to each reader by the access file. It reads its settings from
// the environment, as its README lists them.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	// The drivers of the bucket folder URLs accepted in PORTAL_BUCKET.
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"

	"github.com/bilus/documentation-portal/source"
)

func main() {
	addr, h, err := startup(context.Background(), os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "portalapp:", err)
		os.Exit(1)
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, h))
}

// startup reads the settings from the environment and the access rules from
// the access file, opens the bucket folder, loads the published
// documentation's snapshot, builds its portal handler, wraps it in the
// reloader, which rebuilds it for each settled change until the end of ctx,
// and in the previews handler, and puts the sign-in in front. It returns the
// address to listen on and the handler: the sign-in router.
func startup(ctx context.Context, getenv func(string) string) (string, http.Handler, error) {
	s, err := readSettings(getenv)
	if err != nil {
		return "", nil, err
	}
	rules, err := readAccessFile(s.accessFile)
	if err != nil {
		return "", nil, err
	}
	published, location, err := openBucket(ctx, s.bucketURL, s.previews)
	if err != nil {
		return "", nil, err
	}
	snap, err := source.Load(ctx, published)
	if err != nil {
		return "", nil, err
	}
	b := &builder{configPath: s.configPath, rules: rules, model: s.chat}
	h, err := b.build(snap.Root)
	if err != nil {
		return "", nil, err
	}
	reloader := source.NewReloader(ctx, published, snap.Listing, h, s.refresh, b.build)
	previews := withPreviews(ctx, reloader, location, s.refresh, rules, b.buildPreview)
	h, err = signIn(ctx, s.providers, rules, previews)
	if err != nil {
		return "", nil, err
	}
	return s.addr, h, nil
}
