package nfo

// ArtworkExtension preserves enough provenance to regenerate a marked poster
// from its original after restart or NFO import. Originals live outside the
// prunable image cache; the hash is never interpreted as an arbitrary path.
type ArtworkExtension struct {
	Original string   `xml:"original" json:"original"`
	Profile  string   `xml:"profile" json:"profile"`
	Labels   []string `xml:"label,omitempty" json:"labels,omitempty"`
}
