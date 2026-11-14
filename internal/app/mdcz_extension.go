package app

import (
	"context"
	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/ent"
	"github.com/ppxb/miyabi/internal/mdcz"
	"github.com/ppxb/miyabi/internal/metadata"
)

// Keep the optional extension assembly separate from upstream provider setup.
// Database errors remain fatal; optional storage/connection faults are exposed
// by the extension itself without stopping library browsing or native settings.
func newMDCzExtension(ctx context.Context, db *ent.Client, dir string, native *metadata.Service) (*mdcz.Service, error) {
	return mdcz.New(ctx, db, dir, native)
}

type imageReader interface {
	Image(context.Context, domain.ImageCandidate) (domain.Media, error)
}

// The native settings API never depends on promoted MDCz implementation methods.
type metadataAPI struct {
	native *metadata.Service
	images imageReader
}

func (m metadataAPI) Settings() []metadata.SourceSetting { return m.native.Settings() }
func (m metadataAPI) UpdateSettings(ctx context.Context, settings []metadata.SourceSetting) error {
	return m.native.UpdateSettings(ctx, settings)
}
func (m metadataAPI) Image(ctx context.Context, candidate domain.ImageCandidate) (domain.Media, error) {
	return m.images.Image(ctx, candidate)
}
