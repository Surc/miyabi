package javdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

var reviewMovieID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// MovieComments is an optional capability. Keep it outside the catalogue's
// existing JavDBClient interface so upstream clients and test fakes stay intact.
func (c *Client) MovieComments(ctx context.Context, movieID string, page int) (domain.MovieComments, error) {
	result := domain.MovieComments{Source: "javdb", Page: page, Items: []domain.MovieComment{}}
	if !reviewMovieID.MatchString(movieID) || page < 1 || page > 1000 {
		return result, domain.E(domain.KindInvalid, "无效的影片或评论页码", nil)
	}
	var data struct {
		Reviews json.RawMessage `json:"reviews"`
	}
	if err := c.getJSON(ctx, "/api/v1/movies/"+url.PathEscape(movieID)+"/reviews", url.Values{
		"page": {strconv.Itoa(page)}, "limit": {"20"},
	}, &data); err != nil {
		return result, err
	}
	if len(data.Reviews) == 0 {
		return result, fmt.Errorf("JavDB response missing reviews")
	}
	var reviews []*struct {
		ID        json.RawMessage `json:"id"`
		Username  string          `json:"username"`
		Content   string          `json:"content"`
		CreatedAt string          `json:"created_at"`
		Score     *wireRating     `json:"score"`
		Likes     *int            `json:"likes_count"`
	}
	if err := json.Unmarshal(data.Reviews, &reviews); err != nil {
		return result, err
	}
	// Pagination counts raw records: rating-only entries must not end pagination.
	result.HasMore = len(reviews) >= 20 && page < 1000
	for _, review := range reviews {
		if review == nil || strings.TrimSpace(review.Content) == "" {
			continue
		}
		var id string
		if err := json.Unmarshal(review.ID, &id); err != nil {
			var number json.Number
			if err := json.Unmarshal(review.ID, &number); err != nil {
				return result, fmt.Errorf("invalid JavDB review id")
			}
			id = number.String()
		}
		if id == "" || bytes.Equal(review.ID, []byte("null")) {
			return result, fmt.Errorf("missing JavDB review id")
		}
		comment := domain.MovieComment{ID: id, Author: review.Username, Content: strings.TrimSpace(review.Content), CreatedAt: review.CreatedAt, Likes: review.Likes}
		if review.Score != nil && *review.Score > 0 && *review.Score <= 5 {
			score := float64(*review.Score)
			comment.Rating, comment.RatingMax = &score, 5
		}
		result.Items = append(result.Items, comment)
	}
	return result, nil
}
