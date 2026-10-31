package export

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

func TestVideoPlansDistinguishVersionsAndParts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  []pan.File
		parts  []int
		nested bool
	}{
		{"versions", []pan.File{{ID: "1", Name: "IPX-906-1080p.mp4"}, {ID: "2", Name: "IPX-906-UC-1080p.mp4"}}, []int{0, 0}, false},
		{"same resolution and name", []pan.File{{ID: "1", Name: "IPX-906.mp4"}, {ID: "2", Name: "IPX-906.mp4"}}, []int{0, 0}, false},
		{"explicit gaps", []pan.File{{ID: "2", Name: "IPX-906-CD2.mp4"}, {ID: "10", Name: "IPX-906-CD10.mp4"}}, []int{2, 10}, false},
		{"padded discs", []pan.File{{ID: "1", Name: "IPX-906-CD01.mp4"}, {ID: "2", Name: "IPX-906-CD02.mp4"}}, []int{1, 2}, false},
		{"single remaining part", []pan.File{{ID: "2", Name: "IPX-906-PART2.mp4"}}, []int{2}, false},
		{"letter parts", []pan.File{{ID: "1", Name: "IPX-906-A.mp4"}, {ID: "2", Name: "IPX-906-B.mp4"}}, []int{1, 2}, false},
		{"joined letter parts", []pan.File{{ID: "1", Name: "IPX-906A.mp4"}, {ID: "2", Name: "IPX-906B.mp4"}}, []int{1, 2}, false},
		{"joined three parts", []pan.File{{ID: "1", Name: "IPX-906A.wmv"}, {ID: "2", Name: "IPX-906B.wmv"}, {ID: "3", Name: "IPX-906C.wmv"}}, []int{1, 2, 3}, false},
		{"joined four parts", []pan.File{{ID: "1", Name: "IPX-906A.wmv"}, {ID: "2", Name: "IPX-906B.wmv"}, {ID: "3", Name: "IPX-906C.wmv"}, {ID: "4", Name: "IPX-906D.wmv"}}, []int{1, 2, 3, 4}, false},
		{"unconfirmed joined C", []pan.File{{ID: "3", Name: "IPX-906C.mp4"}}, []int{0}, false},
		{"duplicate joined C versions", []pan.File{{ID: "1", Name: "IPX-906C.mp4"}, {ID: "2", Name: "IPX-906C.mp4"}}, []int{0, 0}, false},
		{"joined gap", []pan.File{{ID: "1", Name: "IPX-906A.wmv"}, {ID: "3", Name: "IPX-906C.wmv"}}, []int{0, 0}, false},
		{"different containers", []pan.File{{ID: "1", Name: "IPX-906A.wmv"}, {ID: "2", Name: "IPX-906B.wmv"}, {ID: "3", Name: "IPX-906C.mp4"}}, []int{1, 2, 0}, true},
		{"separated C version", []pan.File{{ID: "1", Name: "IPX-906-A.mp4"}, {ID: "2", Name: "IPX-906-B.mp4"}, {ID: "3", Name: "IPX-906-C.mp4"}}, []int{1, 2, 0}, true},
		{"bare letter parts", []pan.File{{ID: "1", Name: "A.mp4"}, {ID: "2", Name: "B.mp4"}}, []int{1, 2}, false},
		{"numeric parts", []pan.File{{ID: "1", Name: "IPX-906-01.mp4"}, {ID: "2", Name: "IPX-906-02.mp4"}}, []int{1, 2}, false},
		{"unconfirmed letter", []pan.File{{ID: "1", Name: "IPX-906-A.mp4"}}, []int{0}, false},
		{"mixed", []pan.File{{ID: "1", Name: "IPX-906-CD1.mp4"}, {ID: "2", Name: "IPX-906-CD2.mp4"}, {ID: "3", Name: "IPX-906-UC.mp4"}}, []int{1, 2, 0}, true},
		{"two multipart releases", []pan.File{{ID: "1", ParentID: "a", Name: "IPX-906-CD1.mp4"}, {ID: "2", ParentID: "a", Name: "IPX-906-CD2.mp4"}, {ID: "3", ParentID: "b", Name: "IPX-906-CD1.mp4"}, {ID: "4", ParentID: "b", Name: "IPX-906-CD2.mp4"}}, []int{1, 2, 1, 2}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanVideos("IPX-906", tc.files)
			if err != nil {
				t.Fatal(err)
			}
			var parts []int
			seen := map[string]bool{}
			for _, v := range plan {
				parts = append(parts, v.Part)
				if (v.Dir != "") != tc.nested {
					t.Fatalf("unexpected directory: %+v", v)
				}
				name := filepath.Join(v.Dir, v.Stem+".strm")
				if seen[name] {
					t.Fatalf("colliding outputs: %s", name)
				}
				seen[name] = true
				if part, ok := VideoPart("IPX-906", v.Stem+".strm"); !ok || part != v.Part {
					t.Fatalf("unrecognized output: %+v", v)
				}
			}
			if !reflect.DeepEqual(parts, tc.parts) {
				t.Fatalf("parts=%v want=%v", parts, tc.parts)
			}
			reversed := slices.Clone(tc.files)
			slices.Reverse(reversed)
			again, err := PlanVideos("IPX-906", reversed)
			if err != nil || !reflect.DeepEqual(plan, again) {
				t.Fatalf("unstable plan: %+v %v", again, err)
			}
			if len(plan) > 1 {
				var imported []pan.File
				for _, v := range plan {
					imported = append(imported, pan.File{ID: v.File.ID, Name: v.Stem + ".strm"})
				}
				replayed, err := PlanVideos("IPX-906", imported)
				if err != nil {
					t.Fatal(err)
				}
				paths := func(videos []VideoExport) []string {
					var names []string
					for _, v := range videos {
						names = append(names, filepath.Join(v.Dir, v.Stem))
					}
					slices.Sort(names)
					return names
				}
				if !slices.Equal(paths(plan), paths(replayed)) {
					t.Fatalf("local re-import renamed exports: %v -> %v", paths(plan), paths(replayed))
				}
			}
		})
	}
}

func TestDuplicateDiscPlanDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	_, err := WriteMedia(Config{EmbyDir: root, PublicURL: "http://localhost"}, "AVOP-125", nfo.Movie{}, []pan.File{{ID: "1", Name: "AVOP-125-CD1.mp4"}, {ID: "2", Name: "AVOP-125-CD1.mkv"}}, nil, nil)
	if err == nil {
		t.Fatal("accepted duplicate discs")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("invalid plan changed the export")
	}
}

func TestReportedLTXCopiesRemainIndependentVersions(t *testing.T) {
	for code, second := range map[string]string{"AVOP-125": "UC", "IPX-906": "C"} {
		plan, err := PlanVideos(code, []pan.File{{ID: "ltx", Name: "489155.com@" + code + "-LTX.mp4"}, {ID: "other", Name: code + "-" + second + ".mp4"}})
		if err != nil || len(plan) != 2 {
			t.Fatalf("plan=%v err=%v", plan, err)
		}
		if !strings.Contains(plan[0].Stem, " - LTX [") {
			t.Fatalf("missing literal release label: %s", plan[0].Stem)
		}
		for _, video := range plan {
			if video.Part != 0 || video.Dir != "" {
				t.Fatalf("complete version became a disc: %+v", video)
			}
		}
	}
}

func TestVersionExportMigratesSubtitlesAndPreservesCustomFiles(t *testing.T) {
	cfg := Config{EmbyDir: t.TempDir(), PublicURL: "http://localhost"}
	doc := nfo.Movie{Code: "AVOP-125"}
	first := pan.File{ID: "one", Name: "AVOP-125-1080p.mp4"}
	write := func(files []pan.File) {
		t.Helper()
		if _, err := WriteMedia(cfg, doc.Code, doc, files, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	write([]pan.File{first})
	dir := EmbyMovieDir(cfg.EmbyDir, doc.Code)
	for _, name := range []string{"AVOP-125.zh-CN.srt", "AVOP-1250.strm", "AVOP-125-bonus.strm", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := []pan.File{first, {ID: "two", Name: "AVOP-125-4K.mp4"}}
	write(files)
	plan, _ := PlanVideos(doc.Code, files)
	var original string
	for _, v := range plan {
		if v.File.ID == "one" {
			original = v.Stem
		}
	}
	if body, err := os.ReadFile(filepath.Join(dir, original+".zh-CN.srt")); err != nil || string(body) != "AVOP-125.zh-CN.srt" {
		t.Fatalf("subtitle migration: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "AVOP-125.strm")); !os.IsNotExist(err) {
		t.Fatal("old STRM remains")
	}
	if changed, err := WriteMedia(cfg, doc.Code, doc, files, nil, nil); err != nil || changed {
		t.Fatalf("repeat export=%v %v", changed, err)
	}
	write([]pan.File{first})
	if _, err := os.Stat(filepath.Join(dir, "AVOP-125.zh-CN.srt")); err != nil {
		t.Fatal(err)
	}
	for _, v := range plan {
		if _, err := os.Stat(filepath.Join(dir, v.Stem+".strm")); !os.IsNotExist(err) {
			t.Fatal("obsolete version remains")
		}
	}
	for _, name := range []string{"AVOP-1250.strm", "AVOP-125-bonus.strm", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal("custom file removed", name)
		}
	}
}

func TestMixedVersionsHaveIndependentSidecarsAndCleanup(t *testing.T) {
	cfg := Config{EmbyDir: t.TempDir(), PublicURL: "http://localhost"}
	files := []pan.File{{ID: "one", Name: "IPX-906-CD1.mp4"}, {ID: "two", Name: "IPX-906-CD2.mp4"}, {ID: "full", Name: "IPX-906-UC.mp4"}}
	_, err := WriteMedia(cfg, "IPX-906", nfo.Movie{Code: "IPX-906"}, files, []byte("poster"), []byte("fanart"))
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := PlanVideos("IPX-906", files)
	root := EmbyMovieDir(cfg.EmbyDir, "IPX-906")
	for _, v := range plan {
		for _, name := range []string{v.Stem + ".strm", v.Dir + ".nfo", "poster.jpg", "fanart.jpg"} {
			if _, err := os.Stat(filepath.Join(root, v.Dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	keep := filepath.Join(root, plan[0].Dir, "notes.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMedia(root, "IPX-906"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("removed custom file")
	}
	for _, v := range plan {
		if _, err := os.Stat(filepath.Join(root, v.Dir, v.Stem+".strm")); !os.IsNotExist(err) {
			t.Fatal("version remains")
		}
	}
	if !strings.Contains(plan[2].Stem, "uncensored") {
		t.Fatal("missing release label")
	}
}
