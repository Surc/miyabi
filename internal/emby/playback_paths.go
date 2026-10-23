package emby

import (
	"path"
	"path/filepath"
	"strings"
)

// The export path belongs to Miyabi's OS; the mapped root belongs to Emby's.
// Keep this separate from the legacy notification mapper and never rewrite STRM.
func playbackMediaPath(localPath, localDir, mediaRoot string) string {
	root, err := filepath.Abs(localDir)
	if err != nil {
		return ""
	}
	file, err := filepath.Abs(localPath)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	if mediaRoot == "" {
		return file
	}
	normalized, windows := normalizePlaybackPath(mediaRoot)
	if normalized == "" {
		return ""
	}
	// Do not path.Join a UNC root: it collapses the leading double slash.
	mapped := strings.TrimRight(normalized, "/") + "/" + filepath.ToSlash(rel)
	if windows {
		return strings.ReplaceAll(mapped, "/", `\`)
	}
	return mapped
}

// Compare separators only for recognizable drive/UNC paths. Directory and file
// case stays exact even on Windows, where case-sensitive directories can exist.
// Percent escapes are literal filename characters, never URL-decoded here.
func samePlaybackPath(expected, actual string) bool {
	a, aWindows := normalizePlaybackPath(expected)
	b, bWindows := normalizePlaybackPath(actual)
	return a != "" && b != "" && aWindows == bWindows && a == b
}

func normalizePlaybackPath(value string) (string, bool) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	drive := len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
	unc := strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//")
	if drive || unc {
		value = strings.ReplaceAll(value, `\`, "/")
	}
	// Exported names never contain '..' path components. Reject traversal rather
	// than silently normalizing it into a different media target or UNC share.
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", false
		}
	}
	if drive {
		return strings.ToUpper(value[:1]) + ":" + path.Clean(value[2:]), true
	}
	if unc {
		parts := strings.Split(value[2:], "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" || parts[0] == "." || parts[0] == "?" || parts[1] == "." {
			return "", false
		}
		root := "//" + parts[0] + "/" + parts[1]
		tail := path.Clean("/" + strings.Join(parts[2:], "/"))
		if tail == "/" {
			return root, true
		}
		return root + tail, true
	}
	if !strings.HasPrefix(value, "/") {
		return "", false
	}
	return path.Clean(value), false
}
