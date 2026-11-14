package app

import (
	"context"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/task"
	"github.com/ppxb/miyabi/internal/library/scrape"
	"github.com/ppxb/miyabi/internal/tasks"
)

// Subtitle tasks have a separate projection: their failure never changes a
// successful scan's status or claims the movie failed to enter the library.
func subtitleTaskInfo(record *ent.Task, paused bool) domain.TaskInfo {
	input, err := tasks.DecodePayload[scrape.SubtitlePayload](record.Payload)
	return domain.TaskInfo{
		ID: record.ID, Type: record.Type, Status: string(record.Status), Progress: record.Progress,
		Error: record.Error, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		MovieID: input.MovieID, Code: input.Target.Code, RetryAt: record.RetryAt, RetryCount: record.RetryCount,
		CanRetry: err == nil && record.Status == task.StatusFailed && input.MovieID > 0 && input.Target.FileID != "",
		Paused:   paused && (record.Status == task.StatusQueued || record.Status == task.StatusRunning),
	}
}

func (v *taskViews) listSubtitleTasks(ctx context.Context) ([]domain.TaskInfo, error) {
	recent, err := v.database.Task.Query().Where(task.TypeEQ(string(tasks.KindSubtitle))).
		Order(ent.Desc(task.FieldID)).Limit(20).All(ctx)
	if err != nil {
		return nil, err
	}
	// Never hide unresolved work just because newer successful tasks exist.
	pending, err := v.database.Task.Query().Where(task.TypeEQ(string(tasks.KindSubtitle)),
		task.StatusIn(task.StatusQueued, task.StatusRunning, task.StatusFailed)).All(ctx)
	if err != nil {
		return nil, err
	}
	paused, err := tasks.LibraryPaused(ctx, v.database)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool)
	result := make([]domain.TaskInfo, 0, len(recent)+len(pending))
	for _, record := range append(recent, pending...) {
		if !seen[record.ID] {
			seen[record.ID] = true
			result = append(result, subtitleTaskInfo(record, paused))
		}
	}
	return result, nil
}

func (v *taskViews) retrySubtitle(ctx context.Context, id int) (domain.TaskInfo, error) {
	if err := v.Queue().Lock(ctx); err != nil {
		return domain.TaskInfo{}, err
	}
	defer v.Queue().Unlock()
	err := ent.WithTx(ctx, v.database, func(tx *ent.Tx) error {
		record, err := tx.Task.Get(ctx, id)
		if err != nil {
			return err
		}
		if record.Type != string(tasks.KindSubtitle) || !subtitleTaskInfo(record, false).CanRetry {
			return domain.E(domain.KindConflict, "该字幕任务不可重试或已在处理中", nil)
		}
		pending, err := tx.Task.Query().Where(task.TypeEQ(string(tasks.KindSubtitle)), task.ResourceKeyEQ(record.ResourceKey),
			task.StatusIn(task.StatusQueued, task.StatusRunning)).Exist(ctx)
		if err != nil {
			return err
		}
		if pending {
			return domain.E(domain.KindConflict, "同一字幕目标已有待处理任务", nil)
		}
		return tx.Task.UpdateOneID(id).SetStatus(task.StatusQueued).SetProgress(0).SetRetryCount(0).ClearRetryAt().ClearError().Exec(ctx)
	})
	if err != nil {
		return domain.TaskInfo{}, err
	}
	v.NotifyUI()
	v.WakePool()
	record, err := v.database.Task.Get(ctx, id)
	if err != nil {
		return domain.TaskInfo{}, err
	}
	paused, err := tasks.LibraryPaused(ctx, v.database)
	if err != nil {
		return domain.TaskInfo{}, err
	}
	return subtitleTaskInfo(record, paused), nil
}
