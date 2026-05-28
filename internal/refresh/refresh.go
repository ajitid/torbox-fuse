package refresh

import (
	"context"
	"log"
	"sync"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/store"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

type Manager struct {
	client *torbox.Client
	store  *store.Store
	log    *log.Logger
	mu     sync.Mutex
}

func New(c *torbox.Client, s *store.Store, l *log.Logger) *Manager {
	return &Manager{client: c, store: s, log: l}
}
func (m *Manager) Run(ctx context.Context) ([]media.FileRecord, error) {
	if !m.mu.TryLock() {
		m.log.Printf("refresh already running; skipping")
		return m.store.All(ctx)
	}
	defer m.mu.Unlock()
	var all []media.FileRecord
	for _, typ := range torbox.AllDownloadTypes() {
		items, err := m.client.ListDownloads(ctx, typ)
		if err != nil {
			return nil, err
		}
		recs := media.ProcessItems(m.client, typ, items)
		all = append(all, recs...)
		m.log.Printf("refreshed %s: %d items, %d media files", typ, len(items), len(recs))
	}
	if err := m.store.ReplaceAll(ctx, all); err != nil {
		return nil, err
	}
	return all, nil
}
