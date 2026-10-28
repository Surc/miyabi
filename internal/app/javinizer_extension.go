package app

import (
	"os"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/javinizer"
)

// Only the composition root reads process configuration. The default path does
// not create a remote reader, and never injects a typed nil into an interface.
func newExternalRating() (catalogue.ExternalRatingReader, error) {
	config, err := javinizer.LoadConfig(os.Getenv("MIYABI_JAVINIZER_CONFIG"))
	if err != nil {
		return nil, err
	}
	reader, err := javinizer.New(config)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, nil
	}
	return reader, nil
}
