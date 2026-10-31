package app

import (
	"os"

	"github.com/ppxb/miyabi/internal/catalogue"
	"github.com/ppxb/miyabi/internal/javinizer"
	"github.com/ppxb/miyabi/internal/javlibrary"
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
		browser, err := javlibrary.LoadBrowserConfig(os.Getenv("MIYABI_JAVLIBRARY_BROWSER_CONFIG"))
		if err != nil {
			return nil, err
		}
		client, err := javlibrary.NewBrowserRatingReader(browser)
		if err != nil || client == nil {
			return nil, err
		}
		return client, nil
	}
	return reader, nil
}
