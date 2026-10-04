package portal

import "net/http"

// AccountLink is an account link: a link at the right end of the navigation
// bar, such as the reader's name or a sign-out link, as the account hook
// returns it. A link without a URL shows its label as text.
type AccountLink struct {
	Label string
	URL   string
}

// AccountLinksOf returns the account links of r's reader, as the portal
// handler that serves r found them, or none for a request served by no
// portal handler, so that a route of the portal configuration's Chat shows
// the links of its page's GET.
func AccountLinksOf(r *http.Request) []AccountLink {
	// HOLE(1): give a copy of the account links of r's reader's view
	return nil
}
