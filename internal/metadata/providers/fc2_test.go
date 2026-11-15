package providers

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/metadata"
	"github.com/ppxb/miyabi/internal/netx"
	_ "golang.org/x/image/webp"
)

const fc2ProductPage = `<meta property="og:url" content="https://adult.contents.fc2.com/article/1234567/">
<meta property="og:image" content="https://storage200000.contents.fc2.com/original.jpeg">
<div class="items_article_MainitemThumb"><img src="//contents-thumbnail2.fc2.com/w276/cover.jpeg"><p class="items_article_info">01:24:30</p></div>
<div class="items_article_headerInfo"><h3>Official title</h3></div>
<div class="items_article_softDevice"><h3>Devices</h3><ul><li>PC</li></ul></div>
<div class="items_article_softDevice"><p>販売日 : 2026/03/22</p></div>
<div class="items_article_softDevice"><p>商品ID : FC2 PPV 1234567</p></div>
<section class="items_article_SampleImages">
<a href="//contents-thumbnail2.fc2.com/w1280/preview.jpeg"><img src="//contents-thumbnail2.fc2.com/w160/preview.jpeg"></a>
<a href="https://contents-thumbnail2.fc2.com/w1280/preview.jpeg">Duplicate</a>
<a href="https://foreign.example/unrelated.jpg">Foreign</a>
<a href="javascript:void(0)">Not an image</a>
</section>`

func TestFC2UsesDeclaredOriginalAndReadsSiblingProductDetails(t *testing.T) {
	m, err := parseFC2(fc2ProductPage, "https://adult.contents.fc2.com/", "1234567")
	if err != nil {
		t.Fatal(err)
	}
	if m.Detail.ReleaseDate != "2026-03-22" || m.Detail.Duration != 84 || m.Detail.OriginTitle != "Official title" {
		t.Fatalf("product fields missing: %+v", m.Detail)
	}
	if m.Detail.Cover != "https://storage200000.contents.fc2.com/original.jpeg" || m.Detail.Thumbnail != "https://contents-thumbnail2.fc2.com/w276/cover.jpeg" ||
		len(m.Images) != 3 || m.Images[0].URL != m.Detail.Cover || m.Images[1].URL != m.Detail.Thumbnail || m.Images[0].Layout != domain.CoverSingle {
		t.Fatalf("official original not preferred: %+v", m)
	}
	if !m.Detail.HasPreview || len(m.Detail.PreviewImages) != 1 || m.Detail.PreviewImages[0].Original != "https://contents-thumbnail2.fc2.com/w1280/preview.jpeg" {
		t.Fatalf("preview original missing or duplicated: %+v", m.Detail.PreviewImages)
	}
	body := strings.Replace(fc2ProductPage, "https://storage200000.contents.fc2.com/original.jpeg", "https://foreign.example/cover.jpeg", 1)
	m, err = parseFC2(body, "https://adult.contents.fc2.com/", "1234567")
	if err != nil || m.Detail.Cover != m.Detail.Thumbnail || len(m.Images) != 2 {
		t.Fatalf("thumbnail fallback lost: %+v %v", m, err)
	}
}

func TestFC2RejectsUnconfirmedProductsAndInvalidCodes(t *testing.T) {
	for _, body := range []string{
		strings.Replace(fc2ProductPage, "/article/1234567/", "/article/7654321/", 1),
		strings.Replace(fc2ProductPage, "FC2 PPV 1234567", "FC2 PPV 7654321", 1),
		strings.Replace(fc2ProductPage, "https://adult.contents.fc2.com/article/1234567/", "https://foreign.example/article/1234567/", 1),
		`<div class="items_article_headerInfo"><h3>Unconfirmed title</h3></div>`,
	} {
		if _, err := parseFC2(body, "https://adult.contents.fc2.com/", "1234567"); err == nil {
			t.Fatal("unconfirmed product accepted")
		}
	}
	for _, code := range []string{"", "ABP-123", "FC2-PPV-../1234567", "FC2-1234567A"} {
		if _, err := (&FC2{}).Fetch(t.Context(), domain.MovieRef{Code: code}); !errors.Is(err, metadata.ErrNotFound) {
			t.Fatalf("invalid code fetched: %s %v", code, err)
		}
	}
}

const ppvDataBankProductPage = `<meta property="og:url" content="/article_search.php?id=1234567">
<meta property="og:title" content="Backup title">
<div class="article_title"><a>Archived title</a></div>
<div class="thumb"><img src="/cover.webp"></div>
<ul class="meta"><li>販売日：2025/2/5</li><li>再生時間：01:06:27</li><li>販売者：<a>Seller</a></li></ul>
<ul class="sample_image_area">
<li><a href="/preview.webp"><img src="/small.webp"></a></li>
<li><a href="https://unrelated.example/preview.webp">Foreign image</a></li>
</ul>`

type fc2Transport func(*http.Request) (*http.Response, error)

func (f fc2Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFC2UsesVerifiedArchiveWhenOfficialPageRequiresVerification(t *testing.T) {
	for _, tc := range []struct {
		name, location, officialBody, archiveBody string
		officialStatus, archiveStatus             int
		wantProvider                              string
		wantErr                                   error
	}{
		{name: "official", officialStatus: 200, officialBody: fc2ProductPage, wantProvider: "fc2"},
		{name: "age verification", officialStatus: 303, location: "https://redirect.fc2.com/ekyc_auth/", archiveStatus: 200, archiveBody: ppvDataBankProductPage, wantProvider: "ppvdatabank"},
		{name: "legacy login", officialStatus: 302, location: "/lk/services/id/login", archiveStatus: 200, archiveBody: ppvDataBankProductPage, wantProvider: "ppvdatabank"},
		{name: "verification page", officialStatus: 200, officialBody: `<div class="error-ipblock__ekyc">Age verification</div>`, archiveStatus: 200, archiveBody: ppvDataBankProductPage, wantProvider: "ppvdatabank"},
		{name: "archive miss", officialStatus: 303, location: "https://redirect.fc2.com/ekyc_auth/", archiveStatus: 404, wantErr: metadata.ErrFC2VerificationRequired},
		{name: "wrong product", officialStatus: 303, location: "https://redirect.fc2.com/ekyc_auth/", archiveStatus: 200, archiveBody: strings.Replace(ppvDataBankProductPage, "id=1234567", "id=7654321", 1), wantErr: metadata.ErrFC2VerificationRequired},
		{name: "official not found", officialStatus: 404, wantErr: metadata.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewFC2(nil)
			defer s.Close()
			s.http.Transport = fc2Transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != fc2Base+"article/1234567/" {
					t.Fatalf("unexpected official request: %s", r.URL)
				}
				header := make(http.Header)
				if tc.location != "" {
					header.Set("Location", tc.location)
				}
				return &http.Response{StatusCode: tc.officialStatus, Header: header, Body: io.NopCloser(strings.NewReader(tc.officialBody))}, nil
			})
			s.archive.http.Transport = fc2Transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != ppvDataBankBase+"article/1234567/" || tc.archiveStatus == 0 {
					t.Fatalf("unexpected archive request: %s", r.URL)
				}
				return &http.Response{StatusCode: tc.archiveStatus, Body: io.NopCloser(strings.NewReader(tc.archiveBody))}, nil
			})
			m, err := s.Fetch(t.Context(), domain.MovieRef{Code: "FC2-PPV-1234567", Refresh: true})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || m.Detail.Code != "" ||
					(tc.wantErr == metadata.ErrFC2VerificationRequired && errors.Is(err, metadata.ErrNotFound)) {
					t.Fatalf("incorrect verification or miss result: %+v %v", m, err)
				}
				return
			}
			if err != nil || len(m.Detail.Sources) != 1 || m.Detail.Sources[0] != (domain.SourceID{Provider: tc.wantProvider, ID: "1234567"}) {
				t.Fatalf("incorrect product: %+v %v", m, err)
			}
			if tc.wantProvider == "ppvdatabank" &&
				(m.Detail.Title != "Archived title" || m.Detail.ReleaseDate != "2025-02-05" || m.Detail.Duration != 66 ||
					m.Detail.Maker == nil || m.Detail.Maker.Name != "Seller" || m.Detail.Cover != ppvDataBankBase+"cover.webp" ||
					len(m.Images) != 2 || m.Images[1].URL != ppvDataBankBase+"preview.webp") {
				t.Fatalf("missing archive fields: %+v", m)
			}
		})
	}
}

func TestFC2Live(t *testing.T) {
	codes := os.Getenv("MIYABI_TEST_FC2_CODES")
	if codes == "" {
		t.Skip("set MIYABI_TEST_FC2_CODES for live verification; images are decoded in memory")
	}
	proxy, err := netx.NewProxyManager(netx.ProxyConfig{Enabled: os.Getenv("MIYABI_TEST_PROXY") != "", URL: os.Getenv("MIYABI_TEST_PROXY")})
	if err != nil {
		t.Fatal(err)
	}
	s := NewFC2(proxy)
	defer s.Close()
	for _, code := range strings.Split(codes, ",") {
		t.Run(code, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			m, err := s.Fetch(ctx, domain.MovieRef{Code: code})
			if err != nil {
				t.Fatal(err)
			}
			if !codeid.IsFormatEquivalent(code, m.Detail.Code) || m.Detail.ID != "" || m.Detail.ReleaseDate == "" || len(m.Images) == 0 {
				t.Fatalf("missing identity or core metadata: code=%q, release=%q, duration=%d, images=%d",
					m.Detail.Code, m.Detail.ReleaseDate, m.Detail.Duration, len(m.Images))
			}
			t.Logf("%s: release %s, duration %d, previews %d", code, m.Detail.ReleaseDate, m.Detail.Duration, len(m.Detail.PreviewImages))
			for _, candidate := range m.Images {
				media, err := s.Media(ctx, candidate.URL)
				if err != nil {
					t.Fatal(err)
				}
				decoded, _, err := image.Decode(bytes.NewReader(media.Body))
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s %s: %dx%d", candidate.Role, candidate.URL, decoded.Bounds().Dx(), decoded.Bounds().Dy())
			}
		})
	}
}
