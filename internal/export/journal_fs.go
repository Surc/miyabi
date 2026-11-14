package export

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// OpenRoot confines target reads, writes and renames even if another process
// replaces a checked parent with a link between Lstat and the actual operation.
// Component checks also reject links pointing within the root, which OpenRoot
// by itself intentionally permits.
func (p *Publication) openRoot() (*os.Root, error) {
	r, err := os.OpenRoot(p.Root)
	if err != nil {
		return nil, err
	}
	if err := p.checkRoot(r); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
func (p *Publication) checkRoot(r *os.Root) error {
	current, err := os.Lstat(p.Root)
	if err != nil {
		return err
	}
	opened, err := r.Stat(".")
	if err != nil {
		return err
	}
	if !current.IsDir() || !os.SameFile(current, opened) {
		return errors.New("publication export root was replaced")
	}
	if err := checkRootPath(r, publicationRootMarker, false); err != nil {
		return err
	}
	body, err := r.ReadFile(publicationRootMarker)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(body)) != p.RootID {
		return errors.New("publication export root identity changed")
	}
	return nil
}
func checkRootPath(root *os.Root, name string, missing bool) error {
	if !filepath.IsLocal(name) || name == "." {
		return errors.New("publication path escapes root")
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	path := ""
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := root.Lstat(path)
		if os.IsNotExist(err) && missing {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("publication refuses symbolic link: %s", name)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("publication parent is not a directory: %s", name)
		}
	}
	return nil
}
func rootRead(root *os.Root, name string) (publicationFile, error) {
	if err := checkRootPath(root, name, false); err != nil {
		return publicationFile{}, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return publicationFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPublicationFile {
		return publicationFile{}, fmt.Errorf("unsupported publication file: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return publicationFile{}, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return publicationFile{}, fmt.Errorf("publication file changed while opening: %s", name)
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
func rootHash(root *os.Root, name string) (string, error) {
	file, err := rootRead(root, name)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return fileDigest(file.body), nil
}
func syncRootDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func durableRootWrite(root *os.Root, name string, body []byte, mode fs.FileMode) error {
	if err := checkRootPath(root, name, true); err != nil {
		return err
	}
	dir := filepath.Dir(name)
	var absent []string
	for next := dir; next != "."; next = filepath.Dir(next) {
		_, err := root.Lstat(next)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		absent = append(absent, next)
	}
	for i := len(absent) - 1; i >= 0; i-- {
		if err := root.Mkdir(absent[i], 0755); err != nil && !os.IsExist(err) {
			return err
		}
		if err := syncRootDirectory(root, filepath.Dir(absent[i])); err != nil {
			return err
		}
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	temporary := filepath.Join(dir, ".miyabi-publish-"+id)
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporary) }()
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
	if err := checkRootPath(root, name, true); err != nil {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncRootDirectory(root, dir)
}

func syncRootState(root *os.Root, name string, exists bool) error {
	if exists {
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		if err != nil {
			return err
		}
	}
	// For a never-installed new file even its parent may not exist. Sync the
	// nearest surviving directory; do not create directories during rollback.
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		err := syncRootDirectory(root, dir)
		if !os.IsNotExist(err) || dir == "." {
			return err
		}
	}
}
