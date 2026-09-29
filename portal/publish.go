package portal

// publishedSpec returns raw without its unpublished parts and marker keys, or
// raw itself when it has no marker.
func publishedSpec(raw []byte) ([]byte, error) {
	// HOLE(1): drop the parts that x-doNotPublish marks for main, and every marker key
	return raw, nil
}
