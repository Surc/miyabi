package scrape

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	mediaimage "github.com/ppxb/miyabi/internal/image"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
	"github.com/ppxb/miyabi/internal/tasks"
)

type fixtureDecorator struct{ body []byte }

func (f fixtureDecorator) Decorate(_ context.Context, doc nfo.Movie, files []pan.File, _ []byte) (nfo.Movie, []byte, error) {
	doc.ArtworkExtension = &nfo.ArtworkExtension{Original: "fixture-origin", Profile: "fixture-profile", Labels: []string{"中字"}}
	return doc, f.body, nil
}

func TestDecoratedPosterCheckpointRetainsOriginalFanartAndSynopsis(t *testing.T) {
	store, err := database.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	images, err := mediaimage.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encode := func(shade uint8) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 4, 6))
		for y := range 6 {
			for x := range 4 {
				img.Set(x, y, color.RGBA{shade, shade, shade, 255})
			}
		}
		var body bytes.Buffer
		if err := png.Encode(&body, img); err != nil {
			t.Fatal(err)
		}
		return body.Bytes()
	}
	original := encode(0)
	artwork, err := images.Restore(original, original)
	if err != nil {
		t.Fatal(err)
	}
	input := Payload{Document: nfo.Movie{Code: "TEST-001", Plot: "Saved synopsis"}, Artwork: &artwork}
	originalFanart, originalPoster := artwork.Fanart, artwork.Poster
	service := &Service{db: store.Client, images: images, artworkDecorator: fixtureDecorator{body: encode(255)}}
	job := store.Client.Task.Create().SetType("scrape").SaveX(t.Context())
	if _, err := service.decorateArtwork(t.Context(), job.ID, &input, []pan.File{{Name: "TEST-001-C.mp4"}}, original); err != nil {
		t.Fatal(err)
	}
	saved := store.Client.Task.GetX(t.Context(), job.ID)
	checkpoint, err := tasks.DecodePayload[Payload](saved.Payload)
	if err != nil || checkpoint.Artwork == nil || checkpoint.Artwork.Fanart != originalFanart || checkpoint.Artwork.Poster == originalPoster || checkpoint.Document.Plot != "Saved synopsis" || checkpoint.Document.ArtworkExtension == nil {
		t.Fatalf("invalid checkpoint: %+v %v", checkpoint, err)
	}
	if !images.TryLockArtwork() {
		t.Fatal("artwork lock leaked")
	}
	images.UnlockArtwork()
}

func TestExtensionMetadataPreservationRespectsIdentityAndNativeRatingPolicy(t *testing.T) {
	previous := &nfo.Movie{Code: "TEST-001", Plot: "Saved synopsis", Rating: 4.5, RatingMax: 5, RatingSource: "javdb"}
	next := nfo.Movie{Code: "TEST-001", IDs: []nfo.UniqueID{{Type: "mdcz", Value: "TEST-001"}}}
	preserveExtensionMetadata(&next, previous)
	if next.Plot != previous.Plot || next.Rating != previous.Rating || next.RatingMax != 5 {
		t.Fatalf("lost extension metadata: %+v", next)
	}
	native := nfo.Movie{Code: "TEST-001"}
	preserveExtensionMetadata(&native, previous)
	if native.Rating != 0 || native.Plot != previous.Plot {
		t.Fatal("changed native rating refresh")
	}
	corrected := nfo.Movie{Code: "TEST-002"}
	preserveExtensionMetadata(&corrected, previous)
	if corrected.Plot != "" || corrected.Rating != 0 {
		t.Fatal("copied metadata across films")
	}
}
