// Package top serves a bundled snapshot of JavDB TOP250 rankings.
package top

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/ppxb/miyabi/internal/codeid"
)

//go:embed data.json
var snapshot []byte

type Board struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Info struct {
	UpdatedAt       string  `json:"updated_at"`
	SourceURL       string  `json:"source_url"`
	AnnualSourceURL string  `json:"annual_source_url"`
	Boards          []Board `json:"boards"`
}

type Movie struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
	Cover       string `json:"cover"`
	Rank        int    `json:"rank,omitempty"`
}

type Ranking struct {
	BoardID string `json:"board_id"`
	Name    string `json:"name"`
	Rank    int    `json:"rank"`
}

type Page struct {
	Board Board   `json:"board"`
	Total int     `json:"total"`
	Items []Movie `json:"items"`
}

type catalogue struct {
	info     Info
	boards   map[string]Page
	rankings map[string][]Ranking
}

var load = sync.OnceValue(func() catalogue {
	var data struct {
		Info
		Boards []struct {
			ID    string   `json:"id"`
			Name  string   `json:"name"`
			Codes []string `json:"codes"`
		} `json:"boards"`
		Movies []Movie `json:"movies"`
	}
	if err := json.Unmarshal(snapshot, &data); err != nil {
		panic(err) // A malformed bundled snapshot is a build error.
	}
	films := make(map[string]Movie, len(data.Movies))
	for _, movie := range data.Movies {
		movie.Code = codeid.Normalize(movie.Code)
		films[movie.Code] = movie
	}
	result := catalogue{
		info: data.Info, boards: make(map[string]Page, len(data.Boards)),
		rankings: make(map[string][]Ranking, len(films)),
	}
	result.info.Boards = make([]Board, 0, len(data.Boards))
	for _, board := range data.Boards {
		metadata := Board{board.ID, board.Name, len(board.Codes)}
		result.info.Boards = append(result.info.Boards, metadata)
		items := make([]Movie, 0, len(board.Codes))
		seen := make(map[string]bool, len(board.Codes))
		for index, raw := range board.Codes {
			code := codeid.Normalize(raw)
			movie := films[code]
			movie.Rank = index + 1
			items = append(items, movie)
			// Preserve source positions, but show only the best rank if a board repeats a film.
			if !seen[code] {
				result.rankings[code] = append(result.rankings[code], Ranking{board.ID, board.Name, movie.Rank})
				seen[code] = true
			}
		}
		result.boards[board.ID] = Page{metadata, len(items), items}
	}
	return result
})

func Metadata() Info { return load().info }

// Movies returns source positions unchanged, including any repeated entries.
// Offset and limit are validated by the API caller.
func Movies(boardID string, offset, limit int) (Page, bool) {
	page, found := load().boards[boardID]
	if !found {
		return Page{}, false
	}
	start := min(offset, len(page.Items))
	end := min(start+limit, len(page.Items))
	page.Items = page.Items[start:end]
	return page, true
}

func Rankings(code string) []Ranking {
	items := load().rankings[codeid.Normalize(code)]
	if items == nil {
		return []Ranking{}
	}
	return items
}
