package domain

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type ED2KResource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	URI       string `json:"uri"`
	CreatedAt string `json:"created_at"`
}

type ED2KPage struct {
	Items   []ED2KResource `json:"items"`
	Page    int            `json:"page"`
	HasMore bool           `json:"has_more"`
}

var ed2kFileLink = regexp.MustCompile(`(?i)ed2k://\|file\|([^|<>\p{Cc}]+)\|([0-9]+)\|([a-f0-9]{32})\|(?:h=[a-z2-7]{32}\||p=[a-f0-9]{32}(?::[a-f0-9]{32})*\|){0,2}/`)

// ParseED2K validates a single file link and preserves its submission URI.
func ParseED2K(uri string) (ED2KResource, bool) {
	fields := ed2kFileLink.FindStringSubmatch(uri)
	if fields == nil || fields[0] != uri {
		return ED2KResource{}, false
	}
	return ed2kResource(fields)
}

func ED2KLinks(text string) []ED2KResource {
	var links []ED2KResource
	for _, fields := range ed2kFileLink.FindAllStringSubmatch(html.UnescapeString(text), -1) {
		if link, ok := ed2kResource(fields); ok {
			links = append(links, link)
		}
	}
	return links
}

func ed2kResource(fields []string) (ED2KResource, bool) {
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size <= 0 {
		return ED2KResource{}, false
	}
	name := fields[1]
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "/\\|") || strings.ContainsFunc(name, unicode.IsControl) {
		return ED2KResource{}, false
	}
	id := "ed2k:" + strings.ToLower(fields[3]) + ":" + strconv.FormatInt(size, 10)
	return ED2KResource{ID: id, Name: name, Size: size, URI: fields[0]}, true
}
