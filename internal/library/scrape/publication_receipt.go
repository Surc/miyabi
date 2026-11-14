package scrape

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/tasks"
)

// PublicationCommitted distinguishes a proven rollback from an unknown result.
// A missing/corrupt task or a receipt belonging to another operation is unknown,
// never proof that overwriting exported files is safe. Scrape retries preserve
// completed payloads, and this task cannot be cleaned before journal finalization.
func PublicationCommitted(ctx context.Context, db *ent.Client, publication *export.Publication) (bool, error) {
	record, err := db.Task.Get(ctx, publication.TaskID)
	if err != nil {
		return false, fmt.Errorf("read publication receipt: %w", err)
	}
	if record.Type != string(tasks.KindScrape) {
		return false, fmt.Errorf("publication task kind does not match")
	}
	input, err := tasks.DecodePayload[Payload](record.Payload)
	if err != nil || input.MovieID != publication.MovieID {
		return false, fmt.Errorf("publication task identity is unavailable")
	}
	if input.PublicationID == publication.ID && input.PublicationDigest == publication.Digest && input.Completed {
		return true, nil
	}
	if input.PublicationID == "" && input.PublicationDigest == "" && !input.Completed {
		return false, nil
	}
	return false, fmt.Errorf("publication receipt does not match the pending operation")
}

// The caller holds export/source locks until the database outcome is decided
// and file recovery completes. Cancellation cannot be taken as proof of rollback.
func (service *Service) finishPublication(ctx context.Context, publication *export.Publication, operationErr error) error {
	if publication == nil {
		return operationErr
	}
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	committed, err := PublicationCommitted(checkCtx, service.db, publication)
	if err != nil {
		return errors.Join(operationErr, err)
	}
	if err := publication.Recover(committed); err != nil {
		return errors.Join(operationErr, err)
	}
	if committed {
		// SQLite may report an ambiguous error after committing. A matching
		// durable receipt is authoritative and prevents an erroneous rollback.
		return nil
	}
	if operationErr == nil {
		return fmt.Errorf("publication did not persist its completion receipt")
	}
	return operationErr
}
