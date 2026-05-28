package fusefs

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/TorBox-App/torbox-rclone/internal/media"
	"github.com/TorBox-App/torbox-rclone/internal/torbox"
)

type URLResolver struct {
	client *torbox.Client
	ttl    time.Duration
	mu     sync.Mutex
	cache  map[string]entry
}
type entry struct {
	url     string
	expires time.Time
}

func NewURLResolver(c *torbox.Client) *URLResolver {
	return &URLResolver{client: c, ttl: 5 * time.Minute, cache: map[string]entry{}}
}
func (r *URLResolver) Resolve(ctx context.Context, rec media.FileRecord) (string, error) {
	key := rec.Key
	now := time.Now()
	r.mu.Lock()
	if e := r.cache[key]; e.url != "" && now.Before(e.expires) {
		r.mu.Unlock()
		return e.url, nil
	}
	r.mu.Unlock()
	u, err := r.client.ResolveDownloadURL(ctx, rec.DownloadLink)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.cache[key] = entry{u, now.Add(r.ttl)}
	r.mu.Unlock()
	return u, nil
}
func (r *URLResolver) Invalidate(rec media.FileRecord) {
	r.mu.Lock()
	delete(r.cache, rec.Key)
	r.mu.Unlock()
}
func expiryStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound
}
