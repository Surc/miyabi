package export

import "fmt"

// PublicationStatus is a safe read-only snapshot; it contains no paths or errors.
type PublicationStatus struct {
	Blocked bool `json:"blocked"`
	Pending int  `json:"pending"`
}

func (m *Manager) PublicationStatus() PublicationStatus {
	if m != nil {
		if value := m.publicationStatus.Load(); value != nil {
			return *value
		}
	}
	return PublicationStatus{}
}

func (m *Manager) refreshPublicationStatus() {
	if recovery := m.publications.Load(); recovery != nil {
		pending, err := DiscoverPublications(recovery.directory)
		m.publicationStatus.Store(&PublicationStatus{Blocked: err != nil || len(pending) > 0, Pending: len(pending)})
	}
}

// publicationRecovery is configured once by the application before workers
// start. The resolver reads a database receipt; the file layer owns no database
// or business-package dependency. Every operation runs under Manager.writeMu.
type publicationRecovery struct {
	directory string
	resolve   func(*Publication) (bool, error)
}

// EnablePublicationRecovery installs the durable publication write barrier.
// It does not recover outside the write lock. Call WithConfig once at startup
// to recover before starting writers; later calls also retry any pending work.
func (m *Manager) EnablePublicationRecovery(directory string, resolve func(*Publication) (bool, error)) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	m.publications.Store(&publicationRecovery{directory: directory, resolve: resolve})
}

// PublicationDirectory is safe inside WithConfig; configuration is immutable
// and atomically published, so this accessor never re-enters the write lock.
func (m *Manager) PublicationDirectory() string {
	if m == nil {
		return ""
	}
	if recovery := m.publications.Load(); recovery != nil {
		return recovery.directory
	}
	return ""
}

func (m *Manager) recoverPublications() error {
	recovery := m.publications.Load()
	if recovery == nil {
		return nil
	}
	if recovery.directory == "" || recovery.resolve == nil {
		m.publicationStatus.Store(&PublicationStatus{Blocked: true})
		return fmt.Errorf("publication recovery is not configured")
	}
	pending, err := DiscoverPublications(recovery.directory)
	if err != nil {
		m.publicationStatus.Store(&PublicationStatus{Blocked: true})
		return err
	}
	m.publicationStatus.Store(&PublicationStatus{Blocked: len(pending) > 0, Pending: len(pending)})
	for _, publication := range pending {
		committed, err := recovery.resolve(publication)
		if err != nil {
			return fmt.Errorf("publication %s requires recovery: %w", publication.ID, err)
		}
		if err := publication.Recover(committed); err != nil {
			return fmt.Errorf("publication %s requires recovery: %w", publication.ID, err)
		}
	}
	if err := ValidatePublicationRoot(recovery.directory, m.Config().EmbyDir); err != nil {
		m.publicationStatus.Store(&PublicationStatus{Blocked: true})
		return err
	}
	m.publicationStatus.Store(&PublicationStatus{})
	return nil
}
