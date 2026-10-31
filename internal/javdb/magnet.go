package javdb

import (
	"context"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/ppxb/miyabi/internal/domain"
)

// Magnets decodes resources in the order returned by JavDB.
func (c *Client) Magnets(ctx context.Context, movieID string) ([]domain.Magnet, error) {
	movieID = strings.TrimSpace(movieID)
	if movieID == "" {
		return nil, errors.New("JavDB movie ID is required")
	}
	var data wireMagnetsData
	if err := c.getJSON(ctx, "/api/v1/movies/"+url.PathEscape(movieID)+"/magnets", nil, &data); err != nil {
		return nil, err
	}
	magnets := make([]domain.Magnet, 0, len(data.Magnets))
	invalid := 0
	for _, item := range data.Magnets {
		value := strings.TrimSpace(item.Hash)
		var hash []byte
		var err error
		if len(value) == 32 {
			hash, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(value))
		} else {
			hash, err = hex.DecodeString(value)
		}
		if err != nil || len(hash) != 20 {
			invalid++
			continue
		}
		var tags []string
		if item.CNSub {
			tags = append(tags, domain.MagnetTagSubtitle)
		}
		if item.HD {
			tags = append(tags, domain.MagnetTagHD)
		}
		magnets = append(magnets, domain.Magnet{
			Hash:        hex.EncodeToString(hash),
			Name:        item.Name,
			Size:        item.SizeMiB * 1024 * 1024,
			HasSubtitle: item.CNSub,
			HD:          item.HD,
			FilesCount:  item.FilesCount,
			CreatedAt:   item.CreatedAt,
			Sources:     []string{domain.MagnetSourceJavDB},
			Tags:        tags,
		})
	}
	if invalid > 0 {
		if len(magnets) == 0 {
			return nil, fmt.Errorf("JavDB returned %d invalid info hashes and no usable magnets", invalid)
		}
		slog.WarnContext(ctx, "ignored invalid JavDB info hashes", "movie_id", movieID, "count", invalid)
	}
	return magnets, nil
}

// Name identifies JavDB as a magnet source.
func (c *Client) Name() string {
	return domain.MagnetSourceJavDB
}

// Find retrieves magnets using the movie reference's JavDB ID.
func (c *Client) Find(ctx context.Context, ref domain.MovieRef) ([]domain.Magnet, error) {
	movieID := strings.TrimSpace(ref.JavDBID)
	if movieID == "" {
		return nil, nil
	}
	return c.Magnets(ctx, movieID)
}
