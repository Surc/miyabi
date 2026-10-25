package emby

import (
	"path/filepath"
	"testing"
)

func TestPlaybackMapsExportPathsIndependentlyOfEmbyOS(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "ABC", "ABC-123", "ABC-123.strm")
	for _, tc := range []struct{ root, want string }{
		{"", file},
		{"/", "/ABC/ABC-123/ABC-123.strm"},
		{"/media/", "/media/ABC/ABC-123/ABC-123.strm"},
		{`d:\Media\`, `D:\Media\ABC\ABC-123\ABC-123.strm`},
		{"D:/Media/", `D:\Media\ABC\ABC-123\ABC-123.strm`},
		{`\\nas\share\Media`, `\\nas\share\Media\ABC\ABC-123\ABC-123.strm`},
		{"//nas/share/", `\\nas\share\ABC\ABC-123\ABC-123.strm`},
		{"/media%20name", "/media%20name/ABC/ABC-123/ABC-123.strm"},
		{"relative", ""},
		{`D:relative`, ""},
		{`\\nas`, ""},
		{`\\?\C:\Media`, ""},
		{"/media/../other", ""},
	} {
		t.Run(tc.root, func(t *testing.T) {
			if got := playbackMediaPath(file, root, tc.root); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, file := range []string{root, filepath.Join(root, "..", "outside.strm")} {
		if got := playbackMediaPath(file, root, "/media"); got != "" {
			t.Fatalf("mapped a file outside export directory: %q", got)
		}
	}
}

func TestPlaybackPathComparisonPreservesFilenameIdentity(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		equal bool
	}{
		{`C:\Media\ABC.strm`, `c:/Media/ABC.strm`, true},
		{`\\nas\share\ABC.strm`, `//nas/share/ABC.strm`, true},
		{"/media/ABC.strm", "/media//./ABC.strm", true},
		{"/media/ABC.strm", "/media/abc.strm", false},
		{`C:\Media\ABC.strm`, `C:\media\ABC.strm`, false},
		{`\\nas\share\ABC.strm`, `\\NAS\share\ABC.strm`, false},
		{"/media%20name/A.strm", "/media name/A.strm", false},
		{`/media/a\b.strm`, `/media/a/b.strm`, false},
		{"/media/ABC.strm", "/media/other/../ABC.strm", false},
		{"/media/A.strm", "media/A.strm", false},
		{"/media/A.strm", "/media/A.strm\x00", false},
		{"", "", false},
	} {
		if got := samePlaybackPath(tc.a, tc.b); got != tc.equal {
			t.Errorf("samePlaybackPath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.equal)
		}
	}
}
