package subtitle

import (
	"path/filepath"
	"strings"
)

// MaxFileSize is the size limit for one subtitle file.
const MaxFileSize = 10 << 20

// Track describes a subtitle after normalization, ready to be stored by the caller.
type Track struct {
	Kind
	Source    string
	SourceURL string
}

// Target locates the exported .strm file a movie's subtitles accompany.
type Target struct {
	Dir  string
	Stem string
	Code string
	// FileID verifies that a queued export still points at its original STRM.
	FileID string
	// Version restricts version-specific exports to subtitles for that cut.
	// Empty keeps the existing single-video ranking behavior.
	Version VersionTag
	// Uncensored selects subtitles timed for uncensored cuts.
	Uncensored bool
	// HardSubtitled videos already show Chinese subtitles; no online search is made.
	HardSubtitled bool
}

// Path names a subtitle so Emby attaches it to the .strm and reads its
// language: <stem>[.<version>].<language>.<format>. Managed non-Chinese tracks
// carry an additional marker to distinguish them from unrelated sidecars.
func (target Target) Path(kind Kind) string {
	parts := []string{target.Stem}
	switch kind.Language {
	case "en", "ja", "ko":
		parts = append(parts, "miyabi")
	}
	if kind.Version != VersionStandard && kind.Version != "" {
		parts = append(parts, string(kind.Version))
	}
	parts = append(parts, string(kind.Language), kind.Format)
	return filepath.Join(target.Dir, strings.Join(parts, "."))
}

// Kind groups interchangeable subtitles. Emby lists one track per kind, so a
// movie keeps at most one subtitle of each.
type Kind struct {
	Language Language
	Version  VersionTag
	Format   string
}
