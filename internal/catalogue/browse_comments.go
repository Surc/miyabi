package catalogue

import (
	"context"
	"fmt"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
)

func (s *BrowseExtension) Comments(ctx context.Context, source, id, code string, page int, refresh bool) (BrowseCached[domain.MovieComments], error) {
	code = codeid.Normalize(code)
	identity := id
	if source == "javlibrary" {
		identity = code
	}
	key := fmt.Sprintf("%s:%s:%d", source, identity, page)
	return loadBrowseRecord(ctx, s, "comments", key, commentCacheTTL, refresh, func(ctx context.Context) (domain.MovieComments, bool, error) {
		if s.extras == nil {
			return domain.MovieComments{}, false, domain.E(domain.KindUpstream, "外部评论暂不可用", nil)
		}
		comments, err := s.extras.MovieComments(ctx, source, id, code, page)
		return comments, true, err
	})
}
