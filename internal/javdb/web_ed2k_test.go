package javdb

import (
	"slices"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
)

func TestParseWebED2KReadsOnlyFilmResourceRows(t *testing.T) {
	const uri = "ed2k://|file|SSNI-948%E6%B5%8B%E8%AF%95_restored.mp4%20LTX|8165099933|ABCDEF0123456789ABCDEF0123456789|/"
	page := []byte(`<head><link rel="canonical" href="https://javdb.com/v/RPVkK"></head>
	<div id="magnets-content" class="magnet-links">
		<div class="ad"><a href="` + uri + `">ad</a></div>
		<div class="item odd">
			<div class="magnet-name"><a href="` + uri + `"><span class="name">SSNI-948测试_restored.mp4 LTX</span>
				<div class="tags"><span class="tag">ED2K</span><span class="tag">高清</span></div></a></div>
			<div class="date"><span class="time">2026-11-15</span></div>
		</div>
		<div class="item"><div class="magnet-name"><a href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">BT</a></div></div>
	</div>
	<div class="reviews"><a href="ed2k://|file|SSNI-948.mp4|123|abcdef0123456789abcdef0123456790|/">comment</a></div>`)
	links, err := parseWebED2K(page, "RPVkK")
	if err != nil || len(links) != 1 {
		t.Fatalf("links=%+v error=%v", links, err)
	}
	got := links[0]
	if got.ID != "ed2k:abcdef0123456789abcdef0123456789:8165099933" ||
		got.URI != uri || got.Name != "SSNI-948测试_restored.mp4 LTX" || got.Size != 8165099933 ||
		got.CreatedAt != "2026-11-15" || got.Source != domain.ED2KSourceJavDB ||
		!slices.Equal(got.Tags, []string{domain.MagnetTagHD}) {
		t.Fatalf("unexpected native resource: %+v", got)
	}
	if _, err := parseWebED2K(page, "different"); err == nil {
		t.Fatal("accepted a resource list from the wrong film")
	}
}
