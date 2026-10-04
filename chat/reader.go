package chat

import "net/http"

// An Asker is whom the question limit counts a question against: a
// signed-in reader, by the reader ID, from any address, or a reader without
// one, by the client. A reader and a client never share an asker, even when
// the reader ID is the client's text.
type Asker struct {
	key string
}

// ReaderAsker returns the asker of the signed-in reader whose reader ID is id.
func ReaderAsker(id string) Asker {
	return Asker{key: "reader " + id}
}

// ClientAsker returns the asker of a reader without a reader ID, by the
// reader's client, such as an IPv4 address.
func ClientAsker(client string) Asker {
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

// askerOf returns the asker of a chat tab whose page session holds readerID
// and client: the signed-in reader with the reader ID readerID, whatever the
// client, or for an empty readerID, the client.
func askerOf(readerID, client string) Asker {
	if readerID != "" {
		return ReaderAsker(readerID)
	}
	return ClientAsker(client)
}

// privatePage returns h, the handler of a chat page, which with a reader hook
// answers with Cache-Control: private, since the page holds its reader's ID.
func (c *Chat) privatePage(h http.Handler) http.Handler {
	if c.reader == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private")
		h.ServeHTTP(w, r)
	})
}
