package refresh

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/store"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

const (
	fingerprintHeadWindow = 5
	fingerprintMaxWindow  = 64
)

type sourceBaseline struct {
	count  int
	window int
	token  string
}

type RunResult struct {
	Records             []media.FileRecord
	VisibleMediaChanged bool
}

type Manager struct {
	client    *torbox.Client
	store     *store.Store
	log       *log.Logger
	mu        sync.Mutex
	sources   []torbox.DownloadType
	baselines map[torbox.DownloadType]sourceBaseline
	hasRun    bool
}

func New(c *torbox.Client, s *store.Store, l *log.Logger, sources []torbox.DownloadType) *Manager {
	return &Manager{client: c, store: s, log: l, sources: sources, baselines: make(map[torbox.DownloadType]sourceBaseline)}
}

// Run fully lists every configured source, replaces the store, and re-baselines
// the inexpensive change probe. It is serialized with Poll.
func (m *Manager) Run(ctx context.Context) (RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var all []media.FileRecord
	baselines := make(map[torbox.DownloadType]sourceBaseline, len(m.sources))
	for _, typ := range m.sources {
		items, err := m.client.ListDownloads(ctx, typ)
		if err != nil {
			return RunResult{}, err
		}
		baselines[typ] = baselineFor(items)
		recs := media.ProcessItems(m.client, typ, items)
		all = append(all, recs...)
		m.log.Printf("refreshed %s: %d items, %d media files", typ, len(items), len(recs))
	}
	old, err := m.store.All(ctx)
	if err != nil {
		return RunResult{}, err
	}
	if err := m.store.ReplaceAll(ctx, all); err != nil {
		return RunResult{}, err
	}
	changed := m.hasRun && visibleMediaChanged(old, all)
	m.hasRun = true
	m.baselines = baselines
	return RunResult{Records: all, VisibleMediaChanged: changed}, nil
}

// Poll cheaply checks all configured source libraries. It returns true only
// when a full Run is required; partial probe results never alter a baseline.
func (m *Manager) Poll(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fullRefreshRequired := false
	for _, typ := range m.sources {
		baseline, ok := m.baselines[typ]
		if !ok {
			fullRefreshRequired = true
			continue
		}
		changed, err := m.probe(ctx, typ, baseline)
		if err != nil {
			return false, err
		}
		fullRefreshRequired = fullRefreshRequired || changed
	}
	return fullRefreshRequired, nil
}

func (m *Manager) probe(ctx context.Context, typ torbox.DownloadType, baseline sourceBaseline) (bool, error) {
	if baseline.count <= baseline.window {
		items, err := m.client.ListDownloadsPage(ctx, typ, baseline.window+1, 0)
		if err != nil {
			return false, err
		}
		if len(items) != baseline.count {
			return true, nil
		}
		return renderFingerprint(baseline.count, items, tailID(items)) != baseline.token, nil
	}

	boundary, err := m.client.ListDownloadsPage(ctx, typ, 2, baseline.count-1)
	if err != nil {
		return false, err
	}
	if len(boundary) != 1 {
		return true, nil
	}
	head, err := m.client.ListDownloadsPage(ctx, typ, baseline.window, 0)
	if err != nil {
		return false, err
	}
	return renderFingerprint(baseline.count, head, boundary[0].ID) != baseline.token, nil
}

func baselineFor(items []torbox.Item) sourceBaseline {
	window := windowFor(items)
	head := items
	if len(head) > window {
		head = head[:window]
	}
	return sourceBaseline{count: len(items), window: window, token: renderFingerprint(len(items), head, tailID(items))}
}

func windowFor(items []torbox.Item) int {
	deepest := -1
	for i, item := range items {
		if !item.Cached {
			deepest = i
		}
	}
	window := deepest + 1
	if window < fingerprintHeadWindow {
		window = fingerprintHeadWindow
	}
	if window > fingerprintMaxWindow {
		window = fingerprintMaxWindow
	}
	return window
}

func renderFingerprint(count int, head []torbox.Item, tail string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "n=%d", count)
	for _, item := range head {
		fmt.Fprintf(&b, " %s:%t", item.ID, item.Cached)
	}
	fmt.Fprintf(&b, " tail=%s", tail)
	return b.String()
}

func tailID(items []torbox.Item) string {
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1].ID
}

func visibleMediaChanged(old, new []media.FileRecord) bool {
	return !reflect.DeepEqual(visibleRecords(old), visibleRecords(new))
}

func visibleRecords(records []media.FileRecord) map[string]media.FileRecord {
	out := make(map[string]media.FileRecord)
	for _, record := range records {
		if record.MetadataMediaType == "movie" || record.MetadataMediaType == "series" {
			out[record.Key] = record
		}
	}
	return out
}
