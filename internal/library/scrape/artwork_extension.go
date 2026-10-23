package scrape

import (
	"bytes"
	"context"
	"slices"

	"github.com/ppxb/miyabi/internal/codeid"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

// ArtworkDecorator is optional; old callers keep the native artwork pipeline.
type ArtworkDecorator interface {
	Decorate(context.Context, nfo.Movie, []pan.File, []byte) (nfo.Movie, []byte, error)
}

func (service *Service) decorateArtwork(ctx context.Context, taskID int, input *Payload, videos []pan.File, poster []byte) ([]byte, error) {
	if service.artworkDecorator == nil {
		return poster, nil
	}
	doc, decorated, err := service.artworkDecorator.Decorate(ctx, input.Document, videos, poster)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(poster, decorated) && doc.ArtworkExtension == input.Document.ArtworkExtension {
		return poster, nil
	}
	if err := service.images.LockArtwork(ctx); err != nil {
		return nil, err
	}
	defer service.images.UnlockArtwork()
	if !bytes.Equal(poster, decorated) {
		// Save a poster-derived thumbnail without replacing the original fanart.
		derived, err := service.images.Restore(decorated, decorated)
		if err != nil {
			return nil, err
		}
		input.Artwork.Poster, input.Artwork.Thumbnail = derived.Poster, derived.Thumbnail
	}
	input.Document = doc
	encoded, err := tasks.EncodePayload(input)
	if err != nil {
		return nil, err
	}
	if err := service.db.Task.UpdateOneID(taskID).SetPayload(encoded).Exec(ctx); err != nil {
		return nil, err
	}
	return service.images.ReadURL(input.Artwork.Poster)
}

func preserveExtensionMetadata(next *nfo.Movie, previous *nfo.Movie) {
	if previous == nil || !codeid.IsFormatEquivalent(previous.Code, next.Code) {
		return
	}
	// Optional synopsis absence is not a request to erase a saved introduction.
	if next.Plot == "" {
		next.Plot = previous.Plot
	}
	// MDCz's aggregate lacks a denominator. Keep a known score without changing
	// the native providers' own refresh policy or inventing a source scale.
	if next.Rating == 0 && slices.ContainsFunc(next.IDs, func(id nfo.UniqueID) bool { return id.Type == "mdcz" }) {
		next.Rating, next.RatingSource, next.RatingMax = previous.Rating, previous.RatingSource, previous.RatingMax
	}
}
