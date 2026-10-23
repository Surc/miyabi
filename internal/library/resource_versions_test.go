package library

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
)

func TestOwnedVersionLabelsSeparateLeaksAndUncensored(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"TEST-001-C.mp4", "中文"},
		{"TEST-001-中文字幕-破解.mp4", "中文破解"},
		{"TEST-001-破解.mp4", "破解"},
		{"TEST-001-UMR.mp4", "破解"},
		{"TEST-001-流出.mp4", "流出"},
		{"TEST-001-LEAKED-C.mp4", "中文流出"},
		{"TEST-001-UC.mp4", "中文无码"},
		{"TEST-001-uncensored.mp4", "无码"},
		{"TEST-001-1080p.mp4", ""},
		{"/中文/TEST-001.mp4", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownedVersionLabel(tc.name); got != tc.want {
				t.Fatalf("label=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestResourceVersionsUseCurrentFilesWithoutCombiningDifferentCopies(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	client := store.Client
	film := client.Movie.Create().SetCode("TEST-001").SaveX(ctx)
	orphan := client.Movie.Create().SetCode("TEST-002").SaveX(ctx)
	for i, entry := range []struct{ account, root, name string }{
		{domain.LocalAccountID, "local", "TEST-001-C.mp4"},
		{domain.LocalAccountID, "local", "TEST-001-C-part2.mp4"},
		{"account", "root", "TEST-001-破解.mp4"},
		{"account", "root", "TEST-001.mp4"},
		{"account", "other-root", "TEST-001-C-破解.mp4"},
		{"other-account", "root", "TEST-001-流出.mp4"},
	} {
		client.File.Create().SetFileID(fmt.Sprint(i)).SetName(entry.name).SetSize(1).
			SetAccountID(entry.account).SetRootID(entry.root).SetMovieID(film.ID).ExecX(ctx)
	}
	service := &Service{database: client}
	for _, mounted := range []bool{false, true} {
		labels := []string{"中文"}
		if mounted {
			service.drive = newMountedDrive(t, client, &panStub{}, domain.LibrarySource{
				AccountID: "account", Directory: domain.LibraryDirectory{ID: "root", Name: "Movies", Path: "/Movies"},
			})
			labels = append(labels, "破解")
		}
		got, err := service.ResourceVersions(ctx, []int{film.ID, orphan.ID, film.ID, 9999})
		want := []ResourceVersion{
			{ID: film.ID, Available: true, Labels: labels, Unmarked: mounted},
			{ID: orphan.ID, Labels: []string{}}, {ID: 9999, Labels: []string{}},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("mounted=%t got=%+v err=%v", mounted, got, err)
		}
	}
	if got, err := service.ResourceVersions(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
}
