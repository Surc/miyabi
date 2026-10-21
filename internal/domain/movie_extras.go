package domain

// ExternalRating preserves each site's native scale without changing saved metadata.
// A nil score means an identified film has no rating; Found=false means no match.
type ExternalRating struct {
	Source string   `json:"source"`
	Score  *float64 `json:"score"`
	Max    int      `json:"max"`
	Found  bool     `json:"found"`
}

type MovieComment struct {
	ID        string   `json:"id"`
	Author    string   `json:"author"`
	Content   string   `json:"content"`
	CreatedAt string   `json:"created_at"`
	Rating    *float64 `json:"rating,omitempty"`
	RatingMax int      `json:"rating_max,omitempty"`
	Likes     *int     `json:"likes,omitempty"`
}

type MovieComments struct {
	Source  string         `json:"source"`
	Page    int            `json:"page"`
	HasMore bool           `json:"has_more"`
	Items   []MovieComment `json:"items"`
}
