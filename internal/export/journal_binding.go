package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type publicationRootBinding struct {
	Root string `json:"root"`
	ID   string `json:"identity"`
}

func rootBindingPath(store, root string) string {
	return filepath.Join(store, "roots", fileDigest([]byte(root))+".json")
}
func readRootBinding(store, root string) (string, error) {
	path := rootBindingPath(store, root)
	if err := checkAbsolutePath(path, false); err != nil {
		return "", err
	}
	file, err := readPublicationFile(path)
	if err != nil {
		return "", err
	}
	var binding publicationRootBinding
	if json.Unmarshal(file.body, &binding) != nil || binding.Root != root || !hexID(binding.ID, 16) {
		return "", fmt.Errorf("invalid publication root binding for %s", root)
	}
	return binding.ID, nil
}

// Persist the identity outside the mount even when there are no pending
// publications. A vanished/replaced mount must never silently become a fresh
// export tree at the same path. Explicit root changes get a separate binding.
func boundPublicationRoot(store, root string) (string, error) {
	bound, err := readRootBinding(store, root)
	if err == nil {
		id, err := readRootID(root)
		if err != nil || id != bound {
			return "", fmt.Errorf("bound publication root is unavailable or replaced: %w", errOrConflict(err))
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := safeMkdir(root, 0755); err != nil {
		return "", err
	}
	id, err := ensurePublicationRoot(root)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(publicationRootBinding{Root: root, ID: id})
	if err != nil {
		return "", err
	}
	if err := durableWrite(rootBindingPath(store, root), body, 0600); err != nil {
		return "", err
	}
	return id, nil
}

// ValidatePublicationRoot is read-only and must run under the Manager's write
// lock even when no journal is pending. An unbound, never-published export root
// is allowed; a known root must retain its identity before any writer proceeds.
func ValidatePublicationRoot(storeDir, root string) error {
	store, err := filepath.Abs(storeDir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
	}
	bound, err := readRootBinding(store, root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryRequired, err)
	}
	id, err := readRootID(root)
	if err != nil || id != bound {
		return fmt.Errorf("%w: bound export root is unavailable or replaced: %w", ErrRecoveryRequired, errOrConflict(err))
	}
	return nil
}

// EnsurePublicationRoot binds the root for a durable writer before its first
// publication. Callers must hold the Manager write lock.
func EnsurePublicationRoot(storeDir, root string) (string, error) {
	store, err := filepath.Abs(storeDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return boundPublicationRoot(store, root)
}
