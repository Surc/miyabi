package domain

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode"
)

type DownloadLink struct {
	Key         string `json:"key"`
	URI         string `json:"uri"`
	FromHistory bool   `json:"from_history,omitempty"`
}

func ParseDownloadLink(uri string) (DownloadLink, bool) {
	uri = strings.TrimSpace(uri)
	if link, ok := ParseED2K(uri); ok {
		return DownloadLink{Key: link.ID, URI: link.URI}, true
	}
	if strings.ContainsFunc(uri, unicode.IsControl) {
		return DownloadLink{}, false
	}
	magnet, err := url.Parse(uri)
	if err != nil || magnet.Scheme != "magnet" || magnet.Host != "" || magnet.Path != "" || magnet.Opaque != "" {
		return DownloadLink{}, false
	}
	var key string
	for _, xt := range magnet.Query()["xt"] {
		hash, ok := strings.CutPrefix(strings.ToLower(xt), "urn:btih:")
		if !ok {
			continue
		}
		var decoded []byte
		if len(hash) == 32 {
			decoded, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(hash))
		} else {
			decoded, err = hex.DecodeString(hash)
		}
		if err != nil || len(decoded) != 20 {
			return DownloadLink{}, false
		}
		normalized := hex.EncodeToString(decoded)
		if key != "" && key != normalized {
			return DownloadLink{}, false
		}
		key = normalized
	}
	return DownloadLink{Key: key, URI: uri}, key != ""
}
