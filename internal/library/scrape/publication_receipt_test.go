package scrape

import (
	"testing"

	"github.com/ppxb/miyabi/internal/database"
	"github.com/ppxb/miyabi/internal/export"
	"github.com/ppxb/miyabi/internal/tasks"
)

func TestPublicationReceiptRequiresExactDurableEvidence(t *testing.T) {
	ctx := t.Context()
	store, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, tc := range []struct {
		name               string
		input              Payload
		committed, unknown bool
	}{
		{"not committed", Payload{MetadataPayload: MetadataPayload{MovieID: 7}}, false, false},
		{"committed", Payload{MetadataPayload: MetadataPayload{MovieID: 7}, Completed: true, PublicationID: "operation", PublicationDigest: "digest"}, true, false},
		{"another movie", Payload{MetadataPayload: MetadataPayload{MovieID: 8}}, false, true},
		{"old completed payload", Payload{MetadataPayload: MetadataPayload{MovieID: 7}, Completed: true}, false, true},
		{"different operation", Payload{MetadataPayload: MetadataPayload{MovieID: 7}, Completed: true, PublicationID: "another", PublicationDigest: "digest"}, false, true},
		{"receipt without completion", Payload{MetadataPayload: MetadataPayload{MovieID: 7}, PublicationID: "operation", PublicationDigest: "digest"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := tasks.EncodePayload(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			row := store.Client.Task.Create().SetType(string(tasks.KindScrape)).SetPayload(body).SaveX(ctx)
			got, err := PublicationCommitted(ctx, store.Client, &export.Publication{TaskID: row.ID, MovieID: 7, ID: "operation", Digest: "digest"})
			if got != tc.committed || (err != nil) != tc.unknown {
				t.Fatalf("got %t, %v", got, err)
			}
		})
	}
	if _, err := PublicationCommitted(ctx, store.Client, &export.Publication{TaskID: 99999, MovieID: 7}); err == nil {
		t.Fatal("missing receipt treated as rollback proof")
	}
}
