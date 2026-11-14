package catalogue

import (
	"context"
	"fmt"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

type commentReader interface {
	MovieComments(context.Context, string, int) (domain.MovieComments, error)
}

func (s *Service) javdbCommentPage(ctx context.Context, id string, page int) (domain.MovieComments, error) {
	reader, ok := s.javdb.(commentReader)
	if !ok {
		return domain.MovieComments{Source: "javdb", Page: page, Items: []domain.MovieComment{}},
			domain.E(domain.KindUpstream, "当前 JavDB 客户端不支持评论", nil)
	}
	key := fmt.Sprintf("%s:%d", id, page)
	return cachedJavDB(ctx, s, s.javdbComments, key, func(ctx context.Context) (domain.MovieComments, error) {
		return reader.MovieComments(ctx, id, page)
	})
}

func (s *Service) ED2KResources(ctx context.Context, movieID string, page int) (domain.ED2KPage, error) {
	code, err := s.MovieCode(ctx, movieID)
	if err != nil {
		return domain.ED2KPage{}, err
	}
	comments, err := s.javdbCommentPage(ctx, movieID, page)
	if err != nil {
		return domain.ED2KPage{}, domain.E(domain.KindUpstream, "JavDB 评论区资源读取失败", err)
	}
	result := domain.ED2KPage{Items: []domain.ED2KResource{}, Page: page, HasMore: comments.HasMore}
	seen := make(map[string]bool)
	for _, comment := range comments.Items {
		for _, link := range domain.ED2KLinks(comment.Content) {
			if seen[link.ID] {
				continue
			}
			number, ok := codeid.Parse(link.Name)
			if !ok || !codeid.IsFormatEquivalent(number, code) {
				continue
			}
			seen[link.ID] = true
			link.CreatedAt = comment.CreatedAt
			result.Items = append(result.Items, link)
		}
	}
	return result, nil
}
