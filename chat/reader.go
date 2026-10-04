package chat

import (
	"encoding/base64"
	"net/http"

	"github.com/bilus/documentation-portal/portal"
)

// An Asker is whom the question limit counts a question against: a
// signed-in reader, by the reader ID, from any client, or a reader without
// one, by the client. A reader and a client never share an asker, even when
// the reader ID is the client's text. The zero Asker names nobody, and Ask
// refuses it.
type Asker struct {
	key string
}

// AskerOf returns the asker of a reader with the reader ID readerID, from
// the client client: the signed-in reader, whatever the client, or for an
// empty readerID, the client.
func AskerOf(readerID, client string) Asker {
	if readerID != "" {
		return Asker{key: "reader " + readerID}
	}
	return Asker{key: "client " + client}
}

// readerIDOf returns the reader ID of r's reader from the reader hook, or ""
// without a hook.
func (c *Chat) readerIDOf(r *http.Request) string {
	if c.reader == nil {
		return ""
	}
	return c.reader(r)
}

// sessionReaderID returns readerID as a page session keeps it: in base64,
// since live-templ's signer turns each byte of a string that is not valid
// UTF-8 into U+FFFD, which could give two readers one reader ID.
func sessionReaderID(readerID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(readerID))
}

// readerIDIn returns the reader ID that value, a page session's value from
// sessionReaderID, keeps, or "" for a value that keeps none.
func readerIDIn(value string) string {
	id, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(id)
}

// privatePage returns h, the handler of a chat page, which with a reader hook
// answers with Cache-Control: private, through portal.Private, since the page
// holds its reader's ID.
func (c *Chat) privatePage(h http.Handler) http.Handler {
	if c.reader == nil {
		return h
	}
	return portal.Private(h)
}
