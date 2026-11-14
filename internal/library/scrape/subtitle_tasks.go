package scrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// SubtitlePayload contains one version-specific follow-up. It never contains
// playback tokens or source credentials. The exporter revalidates its STRM file
// identity and current export root before writing anything.
type SubtitlePayload struct {
	MovieID int                 `json:"movie_id"`
	Target  subtitlemeta.Target `json:"target"`
}

// queueSubtitlesTx is part of the movie publication transaction. A crash before
// the worker wake cannot lose work, and Completed scrape replay cannot enqueue
// it a second time. Independent targets have independent retry checkpoints.
func (service *Service) queueSubtitlesTx(ctx context.Context, tx *ent.Tx, input MetadataPayload, videos []pan.File, cfg export.Config) error {
	if service.subtitles == nil || cfg.EmbyDir == "" {
		return nil
	}
	followup := service.subtitleTaskWithConfig(input, videos, cfg)
	if followup == nil {
		return nil
	}
	for _, target := range followup.Targets {
		payload, err := tasks.EncodePayload(SubtitlePayload{MovieID: input.MovieID, Target: target})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		key := "subtitle:" + hex.EncodeToString(digest[:])
		pending, err := tx.Task.Query().Where(task.TypeEQ(string(tasks.KindSubtitle)), task.ResourceKeyEQ(key),
			task.StatusIn(task.StatusQueued, task.StatusRunning)).Exist(ctx)
		if err != nil {
			return err
		}
		if pending {
			continue
		}
		if err := tx.Task.Create().SetType(string(tasks.KindSubtitle)).SetResourceKey(key).SetPayload(payload).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ExportSubtitle is registered in the existing task registry, not a second
// background framework. Transient provider failures use its bounded retry
// policy; failed follow-ups never roll back an already published movie.
func (service *Service) ExportSubtitle(ctx context.Context, job tasks.Job) error {
	input, err := tasks.DecodePayload[SubtitlePayload](job.Payload)
	if err != nil {
		return err
	}
	if input.MovieID <= 0 || input.Target.Code == "" || input.Target.FileID == "" || input.Target.Dir == "" || input.Target.Stem == "" {
		return domain.E(domain.KindInvalid, "字幕任务目标不完整，请重新刮削影片", nil)
	}
	if service.subtitles == nil {
		return domain.E(domain.KindInvalid, "字幕导出服务不可用", nil)
	}
	if err := tasks.Checkpoint(ctx, service.db); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err = service.subtitles.Export(ctx, input.MovieID, input.Target)
	if err != nil {
		return fmt.Errorf("export subtitles: %w", err)
	}
	// Notify even when a retry reused an already-written track: a previous
	// notification failure must not disappear merely because Export returns 0.
	if service.mediaNotifier != nil {
		return service.mediaNotifier.NotifyUpdated(ctx, input.Target.Dir)
	}
	return nil
}
