package scrape

import subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"

// SubtitleTask groups the version-specific targets prepared during publication.
// Delivery uses persistent tasks rather than an in-memory queue.
type SubtitleTask struct {
	MovieID int
	Targets []subtitlemeta.Target
}
