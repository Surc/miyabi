package export

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ppxb/miyabi/internal/codeid"
	subtitlemeta "github.com/ppxb/miyabi/internal/domain/subtitle"
	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

// VideoExport is shared by media publication and subtitle targeting. Dir is
// relative to the movie directory; Part is zero for a complete video.
type VideoExport struct {
	File pan.File
	Dir  string
	Stem string
	Part int
}

var (
	discMarker   = regexp.MustCompile(`(?i)(?:^|[-_. \[\(])(?:CD|DISC|DISK|DVD|PART|PT)[-_ ]*(0{0,2}[1-9][0-9]{0,2})(?:$|[-_. \]\)])`)
	tailPart     = regexp.MustCompile(`(?i)(?:^|[-_. ])([AB]|[0-9]{1,2})$`)
	joinedPart   = regexp.MustCompile(`(?i)^(.+[0-9])([A-D])$`)
	resolution   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(8K|4K|2160P|1440P|1080P|720P|480P)(?:[^a-z0-9]|$)`)
	releaseLabel = regexp.MustCompile(`(?i)(?:^|[-_. ])(LTX|RESTORED)(?:[-_. ]|$)`)
)

// PlanVideos never infers discs from the number of files. Explicit disc markers
// retain their numbers; ambiguous letter/numeric suffixes need a full sequence.
// Multiple multipart releases get separate movie folders, as Emby cannot stack
// parts alongside unrelated videos in the same folder.
func PlanVideos(code string, files []pan.File) ([]VideoExport, error) {
	files = slices.Clone(files)
	sortFiles(files)
	type member struct {
		file pan.File
		part int
	}
	type group struct {
		key      string
		stem     string
		members  []member
		explicit bool
	}
	var groups []*group
	byKey := make(map[string]*group)
	for _, file := range files {
		base := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
		part, explicit := 0, false
		if match := discMarker.FindStringSubmatchIndex(base); match != nil {
			part, _ = strconv.Atoi(base[match[2]:match[3]])
			base = base[:match[0]] + " " + base[match[1]:]
			explicit = true
		} else if match := tailPart.FindStringSubmatchIndex(base); match != nil {
			head := base[:match[0]]
			parsed, _ := codeid.Parse(head)
			if head == "" || codeid.IsEquivalent(parsed, code) {
				value := strings.ToUpper(base[match[2]:match[3]])
				if value == "A" || value == "B" {
					part = int(value[0]-'A') + 1
				} else {
					part, _ = strconv.Atoi(value)
				}
				base = head
			}
		} else if match := joinedPart.FindStringSubmatch(base); match != nil && codeid.IsFormatEquivalent(match[1], code) {
			part = int(strings.ToUpper(match[2])[0]-'A') + 1
			base = match[1]
		}
		key := videoKey(file)
		if part > 0 {
			key = "parts:" + file.ParentID + ":" + strings.ToLower(strings.Trim(base, "-_. "))
			if !explicit {
				key += ":" + strings.ToLower(filepath.Ext(file.Name))
			}
		}
		g := byKey[key]
		if g == nil {
			g = &group{key: key, stem: strings.Trim(base, "-_. "), explicit: explicit}
			byKey[key], groups = g, append(groups, g)
		}
		g.explicit = g.explicit || explicit
		g.members = append(g.members, member{file: file, part: part})
	}
	var releases []*group
	for _, g := range groups {
		parts := make(map[int]bool)
		duplicate := false
		for _, m := range g.members {
			if m.part > 0 && parts[m.part] {
				if g.explicit {
					return nil, fmt.Errorf("duplicate disc %d in %s; distinguish release names or directories", m.part, m.file.Name)
				}
				duplicate = true
			}
			parts[m.part] = true
		}
		sequence := len(g.members) > 1 && !duplicate
		for i := 1; i <= len(g.members); i++ {
			sequence = sequence && parts[i]
		}
		if !g.explicit && !sequence {
			for _, m := range g.members {
				releases = append(releases, &group{key: videoKey(m.file), stem: strings.TrimSuffix(m.file.Name, filepath.Ext(m.file.Name)), members: []member{{file: m.file}}})
			}
		} else {
			releases = append(releases, g)
		}
	}
	nested := len(releases) > 1 && slices.ContainsFunc(releases, func(g *group) bool { return g.members[0].part > 0 })
	var result []VideoExport
	for _, g := range releases {
		stem, dir := nfo.FileStem(code), ""
		if len(releases) > 1 {
			if VersionDirectory(stem, g.stem) {
				stem = g.stem // Keep generated names stable after a local re-import.
			} else {
				digest := sha256.Sum256([]byte(g.key))
				stem += fmt.Sprintf(" - %s [%x]", videoLabel(g.members[0].file.Name), digest[:6])
			}
			if nested {
				dir = stem
			}
		}
		for _, m := range g.members {
			name := stem
			if m.part > 0 {
				name += fmt.Sprintf("-cd%d", m.part)
			}
			result = append(result, VideoExport{File: m.file, Dir: dir, Stem: name, Part: m.part})
		}
	}
	return result, nil
}

func videoKey(file pan.File) string {
	if file.ID != "" {
		return "file:" + file.ID
	}
	return "file:" + file.ParentID + ":" + file.Name
}

func videoLabel(name string) string {
	var labels []string
	if match := resolution.FindStringSubmatch(name); match != nil {
		labels = append(labels, strings.ToLower(match[1]))
	}
	if match := releaseLabel.FindStringSubmatch(name); match != nil {
		labels = append(labels, strings.ToUpper(match[1]))
	}
	if version := subtitlemeta.DetectVersion(name); version != subtitlemeta.VersionStandard {
		labels = append(labels, string(version))
	}
	if subtitlemeta.HasHardSubtitle(name) {
		labels = append(labels, "C")
	}
	if len(labels) == 0 {
		return "original"
	}
	return strings.Join(labels, " ")
}

const versionName = ` - [a-zA-Z0-9 ]+ \[[0-9a-f]{12}\]`

var (
	exportedVideoSuffix   = regexp.MustCompile(`^(?:` + versionName + `)?(?:-cd([1-9][0-9]*))?\.strm$`)
	exportedVersionSuffix = regexp.MustCompile(`^` + versionName + `$`)
)

// VideoPart recognizes only names written by Miyabi, including legacy discs.
func VideoPart(stem, name string) (int, bool) {
	if !strings.HasPrefix(name, stem) {
		return 0, false
	}
	match := exportedVideoSuffix.FindStringSubmatch(strings.TrimPrefix(name, stem))
	if match == nil {
		return 0, false
	}
	part := 0
	if match[1] != "" {
		var err error
		part, err = strconv.Atoi(match[1])
		if err != nil {
			return 0, false
		}
	}
	return part, true
}

// VersionDirectory recognizes the isolated folders used for multipart versions.
func VersionDirectory(stem, name string) bool {
	return strings.HasPrefix(name, stem) && exportedVersionSuffix.MatchString(strings.TrimPrefix(name, stem))
}

func mediaNFO(stem, name string) bool {
	return name == stem+".nfo" || (strings.HasSuffix(name, ".nfo") && VersionDirectory(stem, strings.TrimSuffix(name, ".nfo")))
}
