package mdcz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"slices"

	"github.com/ppxb/miyabi/internal/atomicfile"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

func digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

// Decorate never marks a previously marked image. The original and derived
// bytes are immutable and content-addressed, independent of cache pruning.
func (s *Service) Decorate(ctx context.Context, doc nfo.Movie, videos []pan.File, poster []byte) (nfo.Movie, []byte, error) {
	cfg := s.config()
	if !cfg.Enabled {
		return doc, poster, nil
	}
	original := poster
	origin := digest(original)
	if doc.ArtworkExtension != nil {
		origin = doc.ArtworkExtension.Original
		if !hashPattern.MatchString(origin) {
			return doc, nil, domain.E(domain.KindInvalid, "封面原图记录无效，请重新刮削", nil)
		}
		var err error
		original, err = os.ReadFile(filepath.Join(s.directory, "original-"+origin+".jpg"))
		if err != nil || digest(original) != origin {
			return doc, nil, domain.E(domain.KindInvalid, "封面原图缺失，请重新刮削以恢复原图", nil)
		}
	} else if err := atomicfile.WriteFile(filepath.Join(s.directory, "original-"+origin+".jpg"), original, 0o600); err != nil {
		return doc, nil, err
	}
	var names []string
	for _, video := range videos {
		names = append(names, video.Name)
	}
	slices.Sort(names)
	key, _ := json.Marshal([]any{Protocol, EngineRevision, doc.Code, origin, cfg.Position, names})
	profile := digest(key)
	result := struct {
		Health
		Poster []byte   `json:"poster"`
		Labels []string `json:"labels"`
	}{}
	if cfg.Badges {
		cached, err := os.ReadFile(filepath.Join(s.directory, "derived-"+profile+".json"))
		if err == nil {
			err = json.Unmarshal(cached, &result)
		}
		fresh := err != nil || result.Protocol != Protocol || result.Revision != EngineRevision || len(result.Poster) == 0
		if fresh {
			if err := s.json(ctx, cfg, "POST", "/v1/poster", map[string]any{"code": doc.Code, "poster": original, "filenames": names, "position": cfg.Position}, &result); err != nil {
				return doc, nil, err
			}
		}
		if result.Protocol != Protocol || result.Revision != EngineRevision || len(result.Poster) == 0 {
			return doc, nil, domain.E(domain.KindUpstream, "封面处理返回不兼容结果", nil)
		}
		for _, label := range result.Labels {
			if !slices.Contains([]string{"中字", "破解", "流出", "无码", "4K", "8K", "1080P"}, label) {
				return doc, nil, fmt.Errorf("unsupported resource label")
			}
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(result.Poster))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			return doc, nil, domain.E(domain.KindUpstream, "封面处理返回了无效图片", nil)
		}
		if fresh {
			cached, err = json.Marshal(result)
			if err != nil {
				return doc, nil, err
			}
			if err := atomicfile.WriteFile(filepath.Join(s.directory, "derived-"+profile+".json"), cached, 0o600); err != nil {
				return doc, nil, err
			}
		}
	} else {
		result.Poster = original
		profile = ""
	}
	// Resource labels have their own provenance; site genres are not evidence
	// about the owned file and must never become an input to badge detection.
	doc.Tags = slices.DeleteFunc(slices.Clone(doc.Tags), func(tag nfo.Tag) bool { return tag.Provider == "mdcz-resource" })
	if previous := doc.ArtworkExtension; previous != nil {
		doc.Genres = slices.DeleteFunc(slices.Clone(doc.Genres), func(name string) bool {
			return slices.Contains(previous.Labels, name) && !slices.ContainsFunc(doc.Tags, func(tag nfo.Tag) bool { return tag.Name == name })
		})
	}
	for _, label := range result.Labels {
		doc.Tags = append(doc.Tags, nfo.Tag{Provider: "mdcz-resource", ID: label, Name: label})
		if !slices.Contains(doc.Genres, label) {
			doc.Genres = append(doc.Genres, label)
		}
	}
	doc.ArtworkExtension = &nfo.ArtworkExtension{Original: origin, Profile: profile, Labels: result.Labels}
	return doc, result.Poster, nil
}
