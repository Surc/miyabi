package export

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ppxb/miyabi/internal/nfo"
	"github.com/ppxb/miyabi/internal/pan"
)

// ErrRecoveryRequired means an export journal must remain a write barrier.
var ErrRecoveryRequired = errors.New("export publication requires recovery")

const publicationRootMarker = ".miyabi-publication-root"
const maxPublicationFile = 64 << 20

// Publication is a durable filesystem plan. The caller holds the export and
// source locks from preparation through SQL receipt arbitration and recovery.
// Only the task owner may decide committed; a missing/unreadable SQL task is
// unknown, not an invitation to call Recover(false).
type Publication struct {
	Version         int                `json:"version"`
	ID              string             `json:"operation_id"`
	Digest          string             `json:"plan_digest"`
	TaskID          int                `json:"task_id"`
	MovieID         int                `json:"movie_id"`
	Root            string             `json:"root"`
	RootID          string             `json:"root_id"`
	Stage           string             `json:"stage"`
	Entries         []PublicationEntry `json:"entries"`
	SubtitleMoves   map[string]string  `json:"subtitle_moves,omitempty"`
	SubtitleDeletes []string           `json:"subtitle_deletes,omitempty"`
	store           string
	// fault is test-only, called after a durable target operation.
	fault func(int) error
}

type PublicationEntry struct {
	Path       string `json:"path"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after,omitempty"`
	BeforeMode uint32 `json:"before_mode,omitempty"`
}

type publicationFile struct {
	body []byte
	mode fs.FileMode
}

// PreparePublication derives the plan with the same exporter used by ordinary
// publication, but runs it against private copies of generated sidecars only.
// oldCode is empty unless the caller has verified that no other movie owns it.
func PreparePublication(storeDir string, cfg Config, taskID, movieID int, code, oldCode string, doc nfo.Movie, videos []pan.File, poster, fanart []byte, managedSubtitles ...string) (_ *Publication, resultErr error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if taskID <= 0 || movieID <= 0 || code == "" {
		return nil, errors.New("invalid publication identity")
	}
	root, err := filepath.Abs(cfg.EmbyDir)
	if err != nil {
		return nil, err
	}
	storeDir, err = filepath.Abs(storeDir)
	if err != nil {
		return nil, err
	}
	if err := safeMkdir(storeDir, 0700); err != nil {
		return nil, err
	}
	rootID, err := boundPublicationRoot(storeDir, root)
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	stage := filepath.Join(storeDir, "stages", id)
	if within(root, stage) || within(root, storeDir) {
		return nil, errors.New("publication private storage must be outside the export root")
	}
	if err := safeMkdir(stage, 0700); err != nil {
		return nil, err
	}
	p := &Publication{Version: 2, ID: id, TaskID: taskID, MovieID: movieID, Root: root, RootID: rootID, Stage: stage, store: storeDir}
	journaled := false
	defer func() {
		if resultErr != nil && !journaled {
			_ = os.RemoveAll(stage)
		}
	}()
	ownedSubtitles := make(map[string]bool)
	for _, path := range managedSubtitles {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if !within(root, absolute) {
			return nil, errors.New("managed subtitle is outside the export root")
		}
		ownedSubtitles[absolute] = true
	}
	before := make(map[string]publicationFile)
	for _, candidate := range []string{code, oldCode} {
		if candidate == "" {
			continue
		}
		subtitles := ownedSubtitles
		// Identity correction cannot infer subtitle timing for a new code.
		// Preserve old-code tracks and their DB records instead of deleting them.
		if candidate != code {
			subtitles = nil
		}
		files, err := publicationMedia(root, candidate, subtitles)
		if err != nil {
			return nil, err
		}
		for name, file := range files {
			before[name] = file
		}
	}
	working := filepath.Join(stage, "working")
	if err := safeMkdir(working, 0700); err != nil {
		return nil, err
	}
	for name, file := range before {
		if err := durableWrite(filepath.Join(working, name), file.body, 0600); err != nil {
			return nil, err
		}
	}
	candidateConfig := cfg
	candidateConfig.EmbyDir = working
	if _, err := WriteMedia(candidateConfig, code, doc, videos, poster, fanart); err != nil {
		return nil, err
	}
	if oldCode != "" && !strings.EqualFold(EmbyMovieDir(root, oldCode), EmbyMovieDir(root, code)) {
		if err := RemoveMedia(EmbyMovieDir(working, oldCode), oldCode); err != nil {
			return nil, err
		}
	}
	after := make(map[string]publicationFile)
	err = filepath.WalkDir(working, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected staged file type: %s", path)
		}
		rel, err := filepath.Rel(working, path)
		if err != nil {
			return err
		}
		file, err := readPublicationFile(path)
		if err != nil {
			return err
		}
		after[rel] = file
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths := make(map[string]bool)
	for name := range before {
		paths[name] = true
	}
	for name := range after {
		paths[name] = true
	}
	for name := range paths {
		old, oldOK := before[name]
		next, nextOK := after[name]
		if oldOK == nextOK && bytes.Equal(old.body, next.body) {
			continue
		}
		e := PublicationEntry{Path: name}
		if oldOK {
			e.Before, e.BeforeMode = fileDigest(old.body), uint32(old.mode.Perm())
		}
		if nextOK {
			e.After = fileDigest(next.body)
		}
		p.Entries = append(p.Entries, e)
	}
	// Content writes before STRM exposure, removals after replacements. This is
	// still a multi-file transition, not isolation from external media watchers.
	slices.SortFunc(p.Entries, func(a, b PublicationEntry) int {
		rank := func(e PublicationEntry) int {
			if e.After == "" {
				return 2
			}
			if strings.EqualFold(filepath.Ext(e.Path), ".strm") {
				return 1
			}
			return 0
		}
		if rank(a) != rank(b) {
			return rank(a) - rank(b)
		}
		return strings.Compare(a.Path, b.Path)
	})
	confined, err := p.openRoot()
	if err != nil {
		return nil, err
	}
	defer confined.Close()
	for i, e := range p.Entries {
		if err := checkRootPath(confined, e.Path, true); err != nil {
			return nil, err
		}
		actual, err := rootHash(confined, e.Path)
		if err != nil || actual != e.Before {
			return nil, fmt.Errorf("publication destination changed before preparation: %s: %w", e.Path, errOrConflict(err))
		}
		if e.Before != "" {
			if err := durableWrite(p.blob(i, false), before[e.Path].body, 0600); err != nil {
				return nil, err
			}
		}
		if e.After != "" {
			if err := durableWrite(p.blob(i, true), after[e.Path].body, 0600); err != nil {
				return nil, err
			}
		}
	}
	if err := p.subtitleChanges(code, videos, ownedSubtitles, before, after); err != nil {
		return nil, err
	}
	p.Digest, err = p.planDigest()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	// Once catalog writing starts its outcome can be uncertain (rename may have
	// succeeded before directory sync failed). Never discard recovery blobs.
	journaled = true
	if err := durableWrite(p.catalog(), body, 0600); err != nil {
		return nil, err
	}
	// Working files are not recovery evidence; immutable blobs are. Keep all
	// data private until the journal is fully retired, including cleanup errors.
	return p, nil
}

// Install applies a prepared plan. Errors retain the journal and its blobs.
func (p *Publication) Install() error {
	if err := p.apply(true); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
	}
	return nil
}

// Recover applies the SQL decision idempotently and retires the journal only
// after all target hashes match. Any outside modification blocks recovery.
func (p *Publication) Recover(committed bool) (resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: %w", ErrRecoveryRequired, resultErr)
		}
	}()
	if err := p.apply(committed); err != nil {
		return err
	}
	if err := os.Remove(p.catalog()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := syncDirectory(p.store); err != nil {
		return err
	}
	// An interrupted cleanup only leaves private, unreferenced data; no future
	// export scan sees it and no SQL evidence is needed to interpret it.
	if err := os.RemoveAll(p.Stage); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(p.Stage))
}

func (p *Publication) apply(committed bool) error {
	if err := p.validate(); err != nil {
		return err
	}
	root, err := p.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	// Validate every target/blob before changing any and repeat per entry.
	// All cooperating writers hold Manager's lock. These checks do not provide
	// compare-and-swap isolation from concurrent external filesystem writers;
	// such writers must be stopped during publication/recovery.
	for i, entry := range p.Entries {
		if err := p.verifyEntry(root, i, entry); err != nil {
			return err
		}
	}
	indices := make([]int, len(p.Entries))
	for i := range indices {
		indices[i] = i
	}
	if !committed {
		slices.Reverse(indices)
	}
	for _, i := range indices {
		e := p.Entries[i]
		if err := p.verifyEntry(root, i, e); err != nil {
			return err
		}
		wanted, mode := e.After, fs.FileMode(0644)
		if !committed {
			wanted, mode = e.Before, fs.FileMode(e.BeforeMode)
		}
		if err := p.checkRoot(root); err != nil {
			return err
		}
		current, err := rootHash(root, e.Path)
		if err != nil {
			return err
		}
		if current == wanted {
			// A previous attempt may have renamed successfully but failed fsync.
			// Matching bytes alone are not sufficient to retire its journal.
			if err := syncRootState(root, e.Path, wanted != ""); err != nil {
				return err
			}
			continue
		}
		if wanted == "" {
			if err := root.Remove(e.Path); err != nil {
				return err
			}
			if err := syncRootDirectory(root, filepath.Dir(e.Path)); err != nil {
				return err
			}
		} else {
			body, err := os.ReadFile(p.blob(i, committed))
			if err != nil {
				return err
			}
			if fileDigest(body) != wanted {
				return fmt.Errorf("publication recovery blob changed: %s", e.Path)
			}
			if err := durableRootWrite(root, e.Path, body, mode); err != nil {
				return err
			}
		}
		if p.fault != nil {
			if err := p.fault(i); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Publication) verifyEntry(root *os.Root, i int, e PublicationEntry) error {
	if err := checkRootPath(root, e.Path, true); err != nil {
		return err
	}
	actual, err := rootHash(root, e.Path)
	if err != nil {
		return err
	}
	if actual != e.Before && actual != e.After {
		return fmt.Errorf("publication blocked by external modification: %s", e.Path)
	}
	for _, candidate := range []struct {
		hash  string
		after bool
	}{{e.Before, false}, {e.After, true}} {
		if candidate.hash == "" {
			continue
		}
		path := p.blob(i, candidate.after)
		if err := checkAbsolutePath(path, false); err != nil {
			return err
		}
		hash, err := hashPublicationPath(path)
		if err != nil || hash != candidate.hash {
			return fmt.Errorf("publication recovery data missing or changed: %s: %w", e.Path, errOrConflict(err))
		}
	}
	return nil
}

// DiscoverPublications reads only journal catalogs. Staging directories without
// a catalog were never allowed to mutate exports and are not recovery work.
func DiscoverPublications(storeDir string) (_ []*Publication, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: %w", ErrRecoveryRequired, resultErr)
		}
	}()
	storeDir, err := filepath.Abs(storeDir)
	if err != nil {
		return nil, err
	}
	if err := checkAbsolutePath(storeDir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(storeDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []*Publication
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return nil, errors.New("publication catalog is not a regular file")
		}
		path := filepath.Join(storeDir, entry.Name())
		file, err := readPublicationFile(path)
		if err != nil {
			return nil, err
		}
		var p Publication
		if err := json.Unmarshal(file.body, &p); err != nil {
			return nil, fmt.Errorf("invalid publication catalog: %w", err)
		}
		p.store = storeDir
		if entry.Name() != p.ID+".json" {
			return nil, errors.New("publication catalog identity mismatch")
		}
		if err := p.validate(); err != nil {
			return nil, err
		}
		result = append(result, &p)
	}
	return result, nil
}

func (p *Publication) validate() error {
	if p.Version != 2 {
		return errors.New("unsupported publication journal version; preserve the catalog and original stage for recovery")
	}
	if !hexID(p.ID, 16) || !hexID(p.RootID, 16) || !hexID(p.Digest, 32) || p.TaskID <= 0 || p.MovieID <= 0 || !filepath.IsAbs(p.Root) || !filepath.IsAbs(p.store) {
		return errors.New("invalid publication journal identity")
	}
	stage := filepath.Join(p.store, "stages", p.ID)
	if p.Stage != stage || within(p.Root, p.store) {
		return errors.New("invalid publication storage location")
	}
	for _, path := range []string{p.Root, p.Stage, p.store} {
		if err := checkAbsolutePath(path, false); err != nil {
			return err
		}
	}
	bound, err := readRootBinding(p.store, p.Root)
	if err != nil || bound != p.RootID {
		return fmt.Errorf("publication root binding changed: %w", errOrConflict(err))
	}
	rootID, err := readRootID(p.Root)
	if err != nil || rootID != p.RootID {
		return fmt.Errorf("publication export root identity changed: %w", errOrConflict(err))
	}
	seen := make(map[string]bool)
	for _, e := range p.Entries {
		if !filepath.IsLocal(e.Path) || e.Path == "." || seen[e.Path] || e.Before == "" && e.After == "" || e.Before != "" && !hexID(e.Before, 32) || e.After != "" && !hexID(e.After, 32) {
			return errors.New("invalid publication file plan")
		}
		seen[e.Path] = true
	}
	digest, err := p.planDigest()
	if err != nil || digest != p.Digest {
		return errors.New("publication plan digest mismatch")
	}
	return nil
}
func (p *Publication) planDigest() (string, error) {
	copy := *p
	copy.Digest = ""
	body, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	return fileDigest(body), nil
}
func (p *Publication) catalog() string { return filepath.Join(p.store, p.ID+".json") }
func (p *Publication) blob(i int, after bool) string {
	name := "old"
	if after {
		name = "new"
	}
	return filepath.Join(p.Stage, name, fmt.Sprintf("%06d", i))
}
func fileDigest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func hexID(value string, length int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == length && value == strings.ToLower(value)
}
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func errOrConflict(err error) error {
	if err != nil {
		return err
	}
	return errors.New("content does not match the recorded state")
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func ensurePublicationRoot(root string) (string, error) {
	id, err := readRootID(root)
	if err == nil {
		return id, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	id, err = randomID()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, publicationRootMarker)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return readRootID(root)
	}
	if err != nil {
		return "", err
	}
	_, writeErr := io.WriteString(f, id+"\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	if err := syncDirectory(root); err != nil {
		return "", err
	}
	return id, nil
}
func readRootID(root string) (string, error) {
	path := filepath.Join(root, publicationRootMarker)
	if err := checkAbsolutePath(path, false); err != nil {
		return "", err
	}
	file, err := readPublicationFile(path)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(file.body))
	if !hexID(id, 16) {
		return "", errors.New("invalid publication root marker")
	}
	return id, nil
}

func readPublicationFile(path string) (publicationFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return publicationFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPublicationFile {
		return publicationFile{}, fmt.Errorf("unsupported publication file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return publicationFile{}, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return publicationFile{}, fmt.Errorf("publication file changed while opening: %s", path)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxPublicationFile+1))
	if err != nil {
		return publicationFile{}, err
	}
	if len(body) > maxPublicationFile {
		return publicationFile{}, errors.New("publication file too large")
	}
	return publicationFile{body: body, mode: info.Mode()}, nil
}
func hashPublicationPath(path string) (string, error) {
	file, err := readPublicationFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fileDigest(file.body), nil
}

func checkAbsolutePath(path string, missing bool) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var paths []string
	for next := path; ; next = filepath.Dir(next) {
		paths = append(paths, next)
		if next == filepath.Dir(next) {
			break
		}
	}
	for i := len(paths) - 1; i >= 0; i-- {
		info, err := os.Lstat(paths[i])
		if os.IsNotExist(err) && missing {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("publication refuses symbolic link: %s", paths[i])
		}
		if i > 0 && !info.IsDir() {
			return fmt.Errorf("publication parent is not a directory: %s", paths[i])
		}
	}
	return nil
}
func safeMkdir(path string, mode fs.FileMode) error {
	if err := checkAbsolutePath(path, true); err != nil {
		return err
	}
	// Sync every newly created parent link, not just the final directory.
	var absent []string
	for next := path; ; next = filepath.Dir(next) {
		_, err := os.Lstat(next)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		absent = append(absent, next)
	}
	for i := len(absent) - 1; i >= 0; i-- {
		if err := os.Mkdir(absent[i], mode); err != nil && !os.IsExist(err) {
			return err
		}
		if err := syncDirectory(filepath.Dir(absent[i])); err != nil {
			return err
		}
	}
	return checkAbsolutePath(path, false)
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func durableWrite(path string, body []byte, mode fs.FileMode) (resultErr error) {
	if err := safeMkdir(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := checkAbsolutePath(path, true); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".miyabi-publish-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
