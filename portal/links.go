package portal

// linkURL returns the URL of the page that serves the link target of dest, a
// link in the markdown file at docPath, or false when the link leads nowhere.
// It returns an absolute URL or a fragment link unchanged.
func (s *site) linkURL(docPath string, dest []byte) ([]byte, bool) {
	// HOLE(1): resolve dest against the documentation root and, unless it starts
	// with /, against the file's directory, keeping its query and fragment
	return dest, true
}

// specURL returns the viewer page URL for target, a path inside the
// documentation root that names the configured spec or, in Stoplight's form,
// a part of it: at the operation route for an operation of the published
// spec, else at the spec's start. It reports false for any other target.
func (s *site) specURL(target string) (string, bool) {
	// HOLE(2): match target against the spec path, and route a Stoplight
	// operation link with operationRoute
	return "", false
}

// operationRoute returns the operation route of pointer in spec, such as
// /operations/registerDevice for paths/~1devices/post, or false when spec has
// no such operation.
func operationRoute(spec []byte, pointer string) (string, bool) {
	// HOLE(2): unescape the path and the method of pointer, and look up the
	// operation's operationId in spec
	return "", false
}
