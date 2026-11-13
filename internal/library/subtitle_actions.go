package library

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	subtitlerow "github.com/ppxb/miyabi/internal/ent/subtitle"
)

type SubtitleProvider interface {
	DownloadMissing(context.Context, subtitlemeta.Target, map[subtitlemeta.Kind]bool, func(subtitlemeta.Track, []byte) (bool, error)) (int, error)
	Normalize(raw []byte, format string) ([]byte, string, error)
}

func (s *Service) DownloadMovieSubtitles(ctx context.Context, id, videoID int) (int, error) {
	if s.subtitles == nil {
		return 0, domain.E(domain.KindInvalid, "字幕下载服务未启用", nil)
	}
	var selected SubtitleVideo
	err := s.withSubtitleVideo(ctx, id, videoID, func(_ *os.Root, _ string, video SubtitleVideo) error {
		if !video.CanDownload {
			return domain.E(domain.KindInvalid, "请手动上传与该视频对应的字幕", nil)
		}
		selected = video
		return nil
	})
	if err != nil {
		return 0, err
	}
	existing := make(map[subtitlemeta.Kind]bool, len(selected.Tracks))
	for _, track := range selected.Tracks {
		language := subtitlemeta.Language(track.Language)
		if language == subtitlemeta.LangSimplifiedChinese || language == subtitlemeta.LangTraditionalChinese {
			existing[subtitlemeta.Kind{Language: language, Format: track.Format, Version: selected.target.Version}] = true
		}
	}
	return s.subtitles.DownloadMissing(ctx, selected.target, existing, func(track subtitlemeta.Track, body []byte) (bool, error) {
		var saved bool
		err := s.withSubtitleVideo(ctx, id, videoID, func(root *os.Root, dir string, video SubtitleVideo) error {
			if video.path != selected.path || video.target != selected.target || !video.CanDownload || dir != selected.rootDir {
				return domain.E(domain.KindConflict, "视频版本已变化，请刷新后重试", nil)
			}
			var err error
			saved, err = s.saveMovieSubtitle(ctx, root, dir, id, video, track, body)
			return err
		})
		return saved, err
	})
}

func (s *Service) AddMovieSubtitle(ctx context.Context, id, videoID int, name, language string, raw []byte) error {
	format := subtitlemeta.Format(filepath.Ext(name))
	if format == "" || len(raw) > subtitlemeta.MaxFileSize {
		return domain.E(domain.KindInvalid, "请选择不超过 10 MiB 的 SRT、ASS、SSA 或 VTT 字幕", nil)
	}
	if s.subtitles == nil {
		return domain.E(domain.KindInvalid, "字幕服务未启用", nil)
	}
	body, content, err := s.subtitles.Normalize(raw, format)
	if err != nil {
		return domain.E(domain.KindInvalid, "字幕文件没有可识别的时间轴", err)
	}
	lang := subtitlemeta.Language(language)
	switch lang {
	case subtitlemeta.LangUnknown:
		lang = subtitlemeta.DetectLanguage(name, content)
	case subtitlemeta.LangSimplifiedChinese, subtitlemeta.LangTraditionalChinese, "en", "ja", "ko":
	default:
		return domain.E(domain.KindInvalid, "不支持所选字幕语言", nil)
	}
	return s.withSubtitleVideo(ctx, id, videoID, func(root *os.Root, dir string, video SubtitleVideo) error {
		track := subtitlemeta.Track{Source: "manual", Kind: subtitlemeta.Kind{Format: format, Language: lang, Version: video.target.Version}}
		saved, err := s.saveMovieSubtitle(ctx, root, dir, id, video, track, body)
		if err == nil && !saved {
			return domain.E(domain.KindConflict, "此版本已有同语言、同格式字幕，请先删除后再添加", nil)
		}
		return err
	})
}

func (s *Service) DeleteMovieSubtitle(ctx context.Context, id, videoID int, trackID string) error {
	return s.withSubtitleVideo(ctx, id, videoID, func(root *os.Root, dir string, video SubtitleVideo) error {
		index := slices.IndexFunc(video.Tracks, func(track SubtitleTrack) bool { return track.ID == trackID })
		if index == -1 {
			return domain.E(domain.KindNotFound, "此视频版本没有这条字幕", nil)
		}
		track := video.Tracks[index]
		path := filepath.Join(video.target.Dir, track.Name)
		temporary := filepath.Join(video.target.Dir, ".miyabi-"+rand.Text()+".tmp")
		if err := root.Rename(path, temporary); err != nil {
			return err
		}
		err := ent.WithTx(ctx, s.database, func(tx *ent.Tx) error {
			if _, err := tx.Subtitle.Delete().Where(subtitlerow.MovieIDEQ(id), subtitlerow.Or(
				subtitlerow.IDEQ(track.recordID), subtitlerow.StoragePathEQ(filepath.Join(dir, path)),
				subtitlerow.StoragePathEQ(filepath.Join(s.exportMgr.Config().EmbyDir, path)),
			)).Exec(ctx); err != nil {
				return err
			}
			return s.notifySubtitleChange(ctx, tx, dir, video.target.Dir)
		})
		if err != nil {
			return errors.Join(err, root.Rename(temporary, path))
		}
		return root.Remove(temporary)
	})
}

func (s *Service) withSubtitleVideo(ctx context.Context, id, videoID int, run func(*os.Root, string, SubtitleVideo) error) error {
	return s.withSubtitleMedia(ctx, id, func(root *os.Root, dir string, videos []SubtitleVideo) error {
		for _, video := range videos {
			if video.ID == videoID {
				return run(root, dir, video)
			}
		}
		return domain.E(domain.KindNotFound, "视频版本不在当前导出目录中", nil)
	})
}

func (s *Service) saveMovieSubtitle(ctx context.Context, root *os.Root, dir string, id int, video SubtitleVideo, track subtitlemeta.Track, body []byte) (bool, error) {
	for _, existing := range video.Tracks {
		if existing.Language == string(track.Language) && existing.Format == track.Format {
			return false, nil
		}
	}
	path := video.target.Path(track.Kind)
	temporary := filepath.Join(video.target.Dir, ".miyabi-"+rand.Text()+".tmp")
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer root.Remove(temporary)
	_, err = file.Write(body)
	if err := errors.Join(err, file.Close()); err != nil {
		return false, err
	}
	// Publish a complete subtitle without replacing an existing file or symlink.
	if err := root.Link(temporary, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	err = ent.WithTx(ctx, s.database, func(tx *ent.Tx) error {
		if err := database.RecordSubtitle(ctx, tx, id, filepath.Join(dir, path), track); err != nil {
			return err
		}
		return s.notifySubtitleChange(ctx, tx, dir, video.target.Dir)
	})
	if err != nil {
		return false, errors.Join(err, root.Remove(path))
	}
	return true, nil
}

func (s *Service) notifySubtitleChange(ctx context.Context, tx *ent.Tx, root, directory string) error {
	if s.mediaNotifier == nil {
		return nil
	}
	return s.mediaNotifier.NotifyUpdatedTx(ctx, tx, filepath.Join(root, directory))
}
