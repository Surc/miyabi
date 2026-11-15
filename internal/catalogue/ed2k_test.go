package catalogue

import (
	"context"
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

type ed2kCommentsStub struct {
	stubProviderWithMagnets
	calls int
	err   error
}

func (s *ed2kCommentsStub) MovieDetail(context.Context, string) (domain.MovieDetail, error) {
	return domain.MovieDetail{Movie: domain.Movie{ID: "1AArv", Code: "SNIS-330"}}, nil
}

func (s *ed2kCommentsStub) MovieComments(_ context.Context, _ string, page int) (domain.MovieComments, error) {
	s.calls++
	if s.err != nil {
		return domain.MovieComments{}, s.err
	}
	return domain.MovieComments{Page: page, HasMore: true, Items: []domain.MovieComment{{
		CreatedAt: "2026-11-13T06:09:28.000Z",
		Content: "LTX ed2k://|file|www.example.test@SNIS-330_restored_wm.mp4|6994439218|ABCDEF0123456789ABCDEF0123456789|/\n" +
			"ed2k://|file|SNIS-330%20duplicate.mp4|6994439218|abcdef0123456789abcdef0123456789|/\n" +
			"ed2k://|file|SNIS-3301.mp4|123|abcdef0123456789abcdef0123456790|/\n" +
			"ed2k://|file|SNIS-330.mp4|0|abcdef0123456789abcdef0123456791|/\n" +
			"ed2k://|file|SNIS-330%0A.mp4|123|abcdef0123456789abcdef0123456792|/",
	}}}, nil
}

func TestED2KResourcesMatchMovieAndShareCommentPage(t *testing.T) {
	upstream := &ed2kCommentsStub{}
	service := newService(nil, upstream, nil, nil, persistedRoute{})
	extras := NewMovieExtras(service)
	defer extras.Close()

	page, err := service.ED2KResources(t.Context(), "1AArv", 2)
	if err != nil || page.Page != 2 || !page.HasMore || len(page.Items) != 1 {
		t.Fatalf("page=%+v error=%v", page, err)
	}
	link := page.Items[0]
	if link.ID != "ed2k:abcdef0123456789abcdef0123456789:6994439218" || link.Size != 6994439218 ||
		link.Name != "www.example.test@SNIS-330_restored_wm.mp4" ||
		link.CreatedAt != "2026-11-13T06:09:28.000Z" || link.Source != domain.ED2KSourceJavDBComments ||
		link.URI != "ed2k://|file|www.example.test@SNIS-330_restored_wm.mp4|6994439218|ABCDEF0123456789ABCDEF0123456789|/" {
		t.Fatalf("link=%+v", link)
	}
	if _, err := extras.MovieComments(t.Context(), "javdb", "1AArv", "", 2); err != nil || upstream.calls != 1 {
		t.Fatalf("comment requests=%d error=%v", upstream.calls, err)
	}
}

type ed2kWebStub struct {
	ed2kCommentsStub
	web    []domain.ED2KResource
	webErr error
}

func (s *ed2kWebStub) MovieED2K(context.Context, string) ([]domain.ED2KResource, error) {
	return s.web, s.webErr
}

func TestED2KResourcesPreferNativeDuplicateAndKeepAvailableSource(t *testing.T) {
	webLink, _ := domain.ParseED2K("ed2k://|file|SNIS-330_restored_wm.mp4%20LTX|6994439218|abcdef0123456789abcdef0123456789|/")
	webLink.Source = domain.ED2KSourceJavDB
	webUnavailable := errors.New("web resource section unavailable")
	for _, test := range []struct {
		name       string
		webError   error
		reviewErr  error
		wantSource string
		more       bool
	}{
		{name: "duplicate", wantSource: domain.ED2KSourceJavDB, more: true},
		{name: "page blocked", webError: webUnavailable, wantSource: domain.ED2KSourceJavDBComments, more: true},
		{name: "comments unavailable", reviewErr: errors.New("comments unavailable"), wantSource: domain.ED2KSourceJavDB},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &ed2kWebStub{webErr: test.webError}
			if test.webError == nil {
				reader.web = []domain.ED2KResource{webLink}
			}
			reader.err = test.reviewErr
			service := newService(nil, reader, nil, nil, persistedRoute{})
			page, err := service.ED2KResources(t.Context(), "1AArv", 1)
			if err != nil || page.HasMore != test.more || len(page.Items) != 1 ||
				page.Items[0].ID != webLink.ID || page.Items[0].Source != test.wantSource {
				t.Fatalf("page=%+v error=%v", page, err)
			}
			if test.wantSource == domain.ED2KSourceJavDB && page.Items[0].URI != webLink.URI {
				t.Fatal("web link was replaced by a comment alias")
			}
		})
	}
}
