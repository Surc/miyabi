package database

import (
	"context"
	"slices"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
)

// OfflineDownloadLink identifies the successful candidate, including after a switch.
func OfflineDownloadLink(record *ent.OfflineDownload) (domain.DownloadLink, bool) {
	hash := record.Hash
	if record.Recovery != nil {
		if record.Recovery.ED2KURI != "" {
			return domain.ParseDownloadLink(record.Recovery.ED2KURI)
		}
		if record.Recovery.CurrentHash != "" {
			hash = record.Recovery.CurrentHash
		}
	}
	return domain.ParseDownloadLink("magnet:?xt=urn:btih:" + hash)
}

// AddDownloadLinks writes only new source identities. The first observed link
// supplies the URI retained for this file.
func AddDownloadLinks(ctx context.Context, tx *ent.Tx, video *ent.File, links []domain.DownloadLink) (int, error) {
	saved := slices.Clone(video.DownloadLinks)
	for _, link := range links {
		if !slices.ContainsFunc(saved, func(item domain.DownloadLink) bool { return item.Key == link.Key }) {
			saved = append(saved, link)
		}
	}
	added := len(saved) - len(video.DownloadLinks)
	if added == 0 {
		return 0, nil
	}
	return added, tx.File.UpdateOneID(video.ID).SetDownloadLinks(saved).Exec(ctx)
}
