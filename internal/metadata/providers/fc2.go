package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/metadata"
	"github.com/ppxb/miyabi/internal/netx"
	"golang.org/x/net/html"
)

const (
	fc2Base         = "https://adult.contents.fc2.com/"
	ppvDataBankBase = "https://ppvdatabank.com/"
	ppvDataBankHost = "ppvdatabank.com"
)

type FC2 struct {
	client
	archive client
}

func NewFC2(proxy *netx.ProxyManager) *FC2 {
	s := &FC2{
		client:  newClient(proxy, fc2Base, "", "fc2.com"),
		archive: newClient(proxy, ppvDataBankBase, "", ppvDataBankHost),
	}
	checkRedirect := s.http.CheckRedirect
	s.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		path := strings.TrimSuffix(req.URL.Path, "/")
		switch req.URL.Hostname() {
		case "redirect.fc2.com":
			if path == "/ekyc_auth" {
				return metadata.ErrFC2VerificationRequired
			}
		case "adult.contents.fc2.com":
			if path == "/lk/services/id/login" {
				return metadata.ErrFC2VerificationRequired
			}
		}
		return checkRedirect(req, via)
	}
	return s
}

func (s *FC2) Close() {
	s.client.Close()
	s.archive.Close()
}

func (s *FC2) Media(ctx context.Context, target string) (domain.Media, error) {
	if u, err := url.Parse(target); err == nil && fc2Host(u.Hostname(), ppvDataBankHost) {
		return s.archive.Media(ctx, target)
	}
	return s.client.Media(ctx, target)
}

func (*FC2) ID() string { return "fc2" }

var fc2Code = regexp.MustCompile(`^FC2-(?:PPV-)?(\d+)$`)
var fc2ProductID = regexp.MustCompile(`^(?:FC2(?:[- ]*PPV)?[- ]*)?([0-9]+)$`)

func (*FC2) Supports(code string) bool { return fc2Code.MatchString(code) }
func (s *FC2) Fetch(ctx context.Context, ref domain.MovieRef) (domain.MovieMetadata, error) {
	parts := fc2Code.FindStringSubmatch(ref.Code)
	if parts == nil {
		return domain.MovieMetadata{}, metadata.ErrNotFound
	}
	id := parts[1]
	body, err := s.get(ctx, s.base+"article/"+id+"/")
	var result domain.MovieMetadata
	if err == nil {
		result, err = parseFC2(string(body), s.base, id)
	}
	if !errors.Is(err, metadata.ErrFC2VerificationRequired) {
		return result, err
	}
	body, archiveErr := s.archive.get(ctx, s.archive.base+"article/"+id+"/")
	if archiveErr == nil {
		result, archiveErr = parsePPVDataBank(string(body), s.archive.base, id)
	}
	if errors.Is(archiveErr, metadata.ErrNotFound) {
		return domain.MovieMetadata{}, fmt.Errorf("PPVDataBank 未收录该商品: %w", err)
	}
	if archiveErr != nil {
		return domain.MovieMetadata{}, errors.Join(err, fmt.Errorf("PPVDataBank: %w", archiveErr))
	}
	return result, nil
}
func parseFC2(body, base, productID string) (domain.MovieMetadata, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return domain.MovieMetadata{}, err
	}
	if class(doc, "items_notfound_header") != nil {
		return domain.MovieMetadata{}, metadata.ErrNotFound
	}
	if class(doc, "error-ipblock__ekyc") != nil {
		return domain.MovieMetadata{}, metadata.ErrFC2VerificationRequired
	}
	assetURL := func(ref string) string { return fc2MediaURL(base, ref, "fc2.com") }
	confirmed := false
	if ref := fc2Meta(doc, "og:url"); ref != "" {
		u, err := url.Parse(assetURL(ref))
		if err != nil || strings.TrimSuffix(u.Path, "/") != "/article/"+productID {
			return domain.MovieMetadata{}, fmt.Errorf("FC2 商品页身份与请求不一致")
		}
		confirmed = true
	}
	header := class(doc, "items_article_headerInfo")
	m := domain.MovieDetail{Movie: domain.Movie{Code: "FC2-" + productID, Title: text(tag(header, "h3")), Sources: []domain.SourceID{{Provider: "fc2", ID: productID}}}, Zone: domain.ZoneFC2}
	if m.Title == "" {
		return domain.MovieMetadata{}, fmt.Errorf("FC2 商品页未返回标题")
	}
	m.OriginTitle = m.Title
	for _, section := range nodes(doc, func(n *html.Node) bool { return hasClass(n, "items_article_softDevice") }) {
		for _, p := range nodes(section, func(n *html.Node) bool { return n.Data == "p" }) {
			key, value, _ := strings.Cut(strings.ReplaceAll(text(p), "：", ":"), ":")
			switch strings.TrimSpace(key) {
			case "Product ID", "商品ID":
				parts := fc2ProductID.FindStringSubmatch(strings.TrimSpace(value))
				if parts == nil || parts[1] != productID {
					return domain.MovieMetadata{}, fmt.Errorf("FC2 商品 ID 与请求不一致")
				}
				confirmed = true
			case "Sale Day", "販売日":
				m.ReleaseDate = releaseDate(value)
			}
		}
	}
	if !confirmed {
		return domain.MovieMetadata{}, fmt.Errorf("FC2 商品页缺少可确认的商品 ID")
	}
	if m.ReleaseDate == "" {
		m.ReleaseDate = releaseDate(text(class(header, "items_article_Releasedate")))
	}
	thumb := class(doc, "items_article_MainitemThumb")
	m.Thumbnail = assetURL(attr(tag(thumb, "img"), "src"))
	m.Cover = assetURL(fc2Meta(doc, "og:image"))
	if m.Cover == "" {
		m.Cover = m.Thumbnail
	}
	if m.Thumbnail == "" {
		m.Thumbnail = m.Cover
	}
	duration := strings.Split(text(class(thumb, "items_article_info")), ":")
	m.Duration = integer(duration[0])
	if len(duration) == 3 {
		m.Duration = m.Duration*60 + integer(duration[1])
	}
	return fc2Metadata(m, class(doc, "items_article_SampleImages"), assetURL), nil
}

func parsePPVDataBank(body, base, productID string) (domain.MovieMetadata, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return domain.MovieMetadata{}, err
	}
	assetURL := func(ref string) string { return fc2MediaURL(base, ref, ppvDataBankHost) }
	u, err := url.Parse(assetURL(fc2Meta(doc, "og:url")))
	if err != nil || u.Hostname() != ppvDataBankHost ||
		(strings.TrimSuffix(u.Path, "/") != "/article/"+productID &&
			(u.Path != "/article_search.php" || u.Query().Get("id") != productID)) {
		return domain.MovieMetadata{}, fmt.Errorf("PPVDataBank 商品页缺少匹配的商品 ID")
	}
	title := text(tag(class(doc, "article_title"), "a"))
	if title == "" {
		title = strings.TrimSpace(fc2Meta(doc, "og:title"))
	}
	if title == "" {
		return domain.MovieMetadata{}, fmt.Errorf("PPVDataBank 商品页未返回标题")
	}
	thumb := assetURL(attr(tag(class(doc, "thumb"), "img"), "src"))
	if thumb == "" {
		thumb = assetURL(fc2Meta(doc, "og:image"))
	}
	m := domain.MovieDetail{Movie: domain.Movie{
		Code: "FC2-" + productID, Title: title, OriginTitle: title,
		Thumbnail: thumb, Cover: thumb, Plot: text(class(doc, "explanation")),
		Sources: []domain.SourceID{{Provider: "ppvdatabank", ID: productID}},
	}, Zone: domain.ZoneFC2}
	for _, li := range nodes(class(doc, "meta"), func(n *html.Node) bool { return n.Data == "li" }) {
		label, value, _ := strings.Cut(strings.ReplaceAll(text(li), "：", ":"), ":")
		switch strings.TrimSpace(label) {
		case "販売日":
			if released, err := time.Parse("2006/1/2", strings.TrimSpace(value)); err == nil {
				m.ReleaseDate = released.Format(time.DateOnly)
			}
		case "再生時間":
			parts := strings.Split(value, ":")
			if len(parts) == 2 {
				m.Duration = integer(parts[0])
			} else if len(parts) == 3 {
				m.Duration = 60*integer(parts[0]) + integer(parts[1])
			}
		case "販売者":
			if name := strings.TrimSpace(value); name != "" {
				m.Maker = &domain.Maker{Provider: "ppvdatabank", Name: name}
			}
		}
	}
	return fc2Metadata(m, class(doc, "sample_image_area"), assetURL), nil
}

func fc2Meta(doc *html.Node, property string) string {
	return attr(first(doc, func(n *html.Node) bool {
		return n.Data == "meta" && attr(n, "property") == property
	}), "content")
}

func fc2Metadata(m domain.MovieDetail, samples *html.Node, assetURL func(string) string) domain.MovieMetadata {
	result := domain.MovieMetadata{Detail: m}
	for _, image := range []string{m.Cover, m.Thumbnail} {
		if image != "" && (len(result.Images) == 0 || result.Images[0].URL != image) {
			result.Images = append(result.Images, domain.ImageCandidate{Provider: "fc2", URL: image, Role: "cover", Layout: domain.CoverSingle})
		}
	}
	seen := make(map[string]bool)
	for _, a := range nodes(samples, func(n *html.Node) bool { return n.Data == "a" }) {
		if href := assetURL(attr(a, "href")); href != "" && !seen[href] {
			seen[href] = true
			result.Images = append(result.Images, domain.ImageCandidate{Provider: "fc2", URL: href, Role: "preview"})
			thumbnail := assetURL(attr(tag(a, "img"), "src"))
			if thumbnail == "" {
				thumbnail = href
			}
			result.Detail.PreviewImages = append(result.Detail.PreviewImages, domain.PreviewImage{Original: href, Thumbnail: thumbnail})
		}
	}
	result.Detail.HasPreview = len(result.Detail.PreviewImages) > 0
	return result
}

func fc2MediaURL(base, ref, host string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(absolute(base, ref))
	if err != nil || u.Scheme != "https" || u.User != nil || !fc2Host(u.Hostname(), host) {
		return ""
	}
	return u.String()
}

func fc2Host(actual, domain string) bool {
	return actual == domain || strings.HasSuffix(actual, "."+domain)
}
