package top

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotBoardsAndMemberships(t *testing.T) {
	info := Metadata()
	if info.UpdatedAt != "2026-01-12" || len(info.Boards) != 23 {
		t.Fatalf("unexpected snapshot metadata: %+v", info)
	}
	for _, board := range info.Boards {
		page, found := Movies(board.ID, 0, 250)
		if !found || board.Count != 250 || page.Total != 250 || len(page.Items) != 250 {
			t.Fatalf("incomplete board %s: %+v", board.ID, page)
		}
		for index, movie := range page.Items {
			if movie.Code == "" || movie.Title == "" || movie.Rank != index+1 {
				t.Fatalf("invalid movie in %s: %+v", board.ID, movie)
			}
			if movie.Cover != "" && !strings.HasPrefix(movie.Cover, "https://c0.jdbstatic.com/thumbs/") {
				t.Fatalf("unexpected artwork source: %s", movie.Cover)
			}
			matches := 0
			for _, ranking := range Rankings(movie.Code) {
				if ranking.BoardID == board.ID {
					matches++
					if ranking.Rank > movie.Rank || ranking.Name != board.Name {
						t.Fatalf("rank does not match board: %+v, %+v", movie, ranking)
					}
				}
			}
			if matches != 1 {
				t.Fatalf("%s has %d memberships in %s", movie.Code, matches, board.ID)
			}
		}
	}
}

func TestOverallUsesPinnedGistOrder(t *testing.T) {
	page, _ := Movies("all", 0, 5)
	want := []string{"LAFBD-41", "ABP-984", "SSNI-497", "IPX-811", "IPX-580"}
	for index, code := range want {
		if page.Items[index].Code != code {
			t.Fatalf("rank %d: got %s, want %s", index+1, page.Items[index].Code, code)
		}
	}
	page, _ = Movies("all", 240, 48)
	if len(page.Items) != 10 || page.Items[0].Rank != 241 || page.Items[9].Rank != 250 {
		t.Fatalf("last page renumbered or incomplete: %+v", page)
	}
	page, _ = Movies("all", 48, 48)
	if len(page.Items) != 48 || page.Items[0].Rank != 49 {
		t.Fatalf("unexpected second page: %+v", page)
	}
	page, found := Movies("all", 1000, 48)
	if !found || page.Items == nil || len(page.Items) != 0 || page.Total != 250 {
		t.Fatalf("out-of-range page must remain an empty list: %+v", page)
	}
	if _, found := Movies("missing", 0, 48); found {
		t.Fatal("unknown board was accepted")
	}
}

func TestRankingsNormalizeCodesAndRetainBoardRanks(t *testing.T) {
	ranks := Rankings(" ipx_811 ")
	want := map[string]int{"all": 4, "2022": 1}
	for _, rank := range ranks {
		if expected, exists := want[rank.BoardID]; exists {
			if rank.Rank != expected {
				t.Fatalf("%s: got rank %d, want %d", rank.BoardID, rank.Rank, expected)
			}
			delete(want, rank.BoardID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing memberships: %v", want)
	}
	for _, spelling := range []string{"FC2-4025269", "fc2-ppv-4025269"} {
		found := false
		for _, rank := range Rankings(spelling) {
			if rank.BoardID == "fc2" && rank.Rank == 1 {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing FC2 rank for %s", spelling)
		}
	}
	encoded, _ := json.Marshal(Rankings("UNKNOWN-99999999"))
	if string(encoded) != "[]" {
		t.Fatalf("unknown movies must return an empty JSON list: %s", encoded)
	}
}
