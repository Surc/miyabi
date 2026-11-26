package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/ent/subscription"
)

type UpdateOptions struct {
	// Status pauses or resumes an actor subscription.
	Status *string
}

func (service *Service) Update(ctx context.Context, id int, opts UpdateOptions) (Item, error) {
	record, err := service.database.Subscription.Query().Where(followedActors(), subscription.IDEQ(id)).Only(ctx)
	if err != nil {
		return Item{}, err
	}
	update := record.Update()
	if opts.Status != nil {
		status := subscription.Status(*opts.Status)
		if status != subscription.StatusActive && status != subscription.StatusPaused {
			return Item{}, domain.E(domain.KindInvalid, "只有演员订阅可以暂停或恢复", nil)
		}
		update.SetStatus(status)
		if status == subscription.StatusActive {
			update.SetNextCheckAt(nextDaily(time.Now(), service.config(ctx).CheckTime)).ClearError()
		}
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("update subscription %d: %w", id, err)
	}
	service.tasks.NotifyMonitorChanged()
	return subscriptionItem(updated), nil
}

// Remove deletes a subscription; a missing ID surfaces as not found. A work is
// recorded once under the first actor that found it, so each work of a removed
// actor moves to another followed actor who has seen it, or is deleted.
func (service *Service) Remove(ctx context.Context, id int) error {
	if err := ent.WithTx(ctx, service.database, func(tx *ent.Tx) error {
		if _, err := tx.Subscription.Query().Where(followedActors(), subscription.IDEQ(id)).Only(ctx); err != nil {
			return err
		}
		works, err := tx.Subscription.Query().Where(subscription.KindEQ(subscription.KindMovie),
			subscription.TargetIDHasPrefix(workTargetPrefix), subscription.OriginIDEQ(id)).All(ctx)
		if err != nil {
			return err
		}
		if len(works) > 0 {
			others, err := tx.Subscription.Query().
				Where(followedActors(), subscription.IDNEQ(id)).All(ctx)
			if err != nil {
				return err
			}
			seenBy := make(map[string]int)
			for _, other := range others {
				for _, movieID := range decodeCursor(other.Cursor).SeenMovieIDs {
					seenBy[movieID] = other.ID
				}
			}
			for _, work := range works {
				if owner, ok := seenBy[publicTargetID(work)]; ok {
					err = tx.Subscription.UpdateOneID(work.ID).SetOriginID(owner).Exec(ctx)
				} else {
					err = tx.Subscription.DeleteOneID(work.ID).Exec(ctx)
				}
				if err != nil {
					return err
				}
			}
		}
		return tx.Subscription.DeleteOneID(id).Exec(ctx)
	}); err != nil {
		return err
	}
	service.tasks.NotifyMonitorChanged()
	return nil
}
