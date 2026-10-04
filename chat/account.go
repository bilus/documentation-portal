package chat

import (
	"encoding/json"

	"github.com/bilus/documentation-portal/portal"
)

// readAccountLinks returns the account links in value, a page's session
// value, or none for a value that does not hold them.
func readAccountLinks(value string) []portal.AccountLink {
	var links []portal.AccountLink
	if err := json.Unmarshal([]byte(value), &links); err != nil {
		return nil
	}
	return links
}
