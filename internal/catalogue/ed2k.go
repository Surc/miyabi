package catalogue

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

type commentReader interface {
	MovieComments(context.Context, string, int) (domain.MovieComments, error)
}

type ed2kReader interface {
	MovieED2K(context.Context, string) ([]domain.ED2KResource, error)
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
	reader, ok := s.javdb.(ed2kReader)
	if page != 1 || !ok {
		return s.commentED2KResources(ctx, movieID, page)
	}

	var web []domain.ED2KResource
	var webErr error
	var loads sync.WaitGroup
	loads.Go(func() {
		web, webErr = s.javdbED2K.get(ctx, movieID, func(ctx context.Context) ([]domain.ED2KResource, error) {
			return reader.MovieED2K(ctx, movieID)
		})
	})
	comments, commentErr := s.commentED2KResources(ctx, movieID, page)
	loads.Wait()
	if err := ctx.Err(); err != nil {
		return domain.ED2KPage{}, err
	}
	if len(web) == 0 {
		if commentErr == nil {
			return comments, nil
		}
		return domain.ED2KPage{}, domain.E(domain.KindOf(commentErr), "JavDB ED2K 资源读取失败", errors.Join(commentErr, webErr))
	}

	result := domain.ED2KPage{
		Items: make([]domain.ED2KResource, 0, len(web)+len(comments.Items)),
		Page:  page, HasMore: comments.HasMore,
	}
	seen := make(map[string]bool)
	for _, resources := range [][]domain.ED2KResource{web, comments.Items} {
		for _, link := range resources {
			if !seen[link.ID] {
				seen[link.ID] = true
				result.Items = append(result.Items, link)
			}
		}
	}
	return result, nil
}

func (s *Service) commentED2KResources(ctx context.Context, movieID string, page int) (domain.ED2KPage, error) {
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
			link.Source = domain.ED2KSourceJavDBComments
			link.CreatedAt = comment.CreatedAt
			result.Items = append(result.Items, link)
		}
	}
	return result, nil
}
