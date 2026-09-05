package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

const (
	DefaultCacheSize = 9 * 1024 * 1024 * 1024 / 2
	DefaultReadAhead = 600 * 1024 * 1024
)

type Config struct {
	APIKey                   string
	MountPath                string
	DataPath                 string
	MediaChangeCheckPollTime time.Duration
	MediaFullResyncTime      time.Duration
	AllowOther               bool
	Version                  string
	CachePath                string
	CacheSize                int64
	ReadAhead                int64
	PlexAccessToken          string
	PlexBaseURL              string
	JellyfinAPIKey           string
	JellyfinBaseURL          string
	WebAppPort               int
	Sources                  []torbox.DownloadType
}

func Load() (Config, error) {
	apiKey := strings.TrimSpace(os.Getenv("TORBOX_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("TORBOX_API_KEY is required")
	}
	mountPath := envDefault("MOUNT_PATH", "./torbox")
	dataPath := envDefault("DATA_PATH", "./torbox-fuse.db")
	plexAccessToken := strings.TrimSpace(os.Getenv("PLEX_ACCESS_TOKEN"))
	jellyfinAPIKey := strings.TrimSpace(os.Getenv("JELLYFIN_API_KEY"))
	pollTime, err := parseMediaChangeCheckPollTime(durationEnv("MEDIA_CHANGE_CHECK_POLL_TIME", "15s"))
	if err != nil {
		return Config{}, err
	}
	resyncTime, err := parseMediaFullResyncTime(durationEnv("MEDIA_FULL_RESYNC_TIME", "24h"))
	if err != nil {
		return Config{}, err
	}
	cacheRoot, _ := os.UserCacheDir()
	if cacheRoot == "" {
		cacheRoot = os.TempDir()
	}
	sources, err := parseSources(envDefault("TORBOX_SOURCES", "torrent"))
	if err != nil {
		return Config{}, err
	}
	webAppPort, err := parsePort(envDefault("WEBAPP_PORT", "4747"))
	if err != nil {
		return Config{}, err
	}
	return Config{
		APIKey:                   apiKey,
		MountPath:                mountPath,
		DataPath:                 dataPath,
		MediaChangeCheckPollTime: pollTime,
		MediaFullResyncTime:      resyncTime,
		AllowOther:               parseBoolEnv(os.Getenv("FUSE_ALLOW_OTHER")),
		Version:                  "dev",
		CachePath:                envDefault("CACHE_PATH", filepath.Join(cacheRoot, "torbox-fuse")),
		CacheSize:                DefaultCacheSize,
		ReadAhead:                DefaultReadAhead,
		PlexAccessToken:          plexAccessToken,
		PlexBaseURL:              envDefault("PLEX_BASE_URL", "http://127.0.0.1:32400"),
		JellyfinAPIKey:           jellyfinAPIKey,
		JellyfinBaseURL:          envDefault("JELLYFIN_BASE_URL", "http://127.0.0.1:8096"),
		WebAppPort:               webAppPort,
		Sources:                  sources,
	}, nil
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func durationEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func parseMediaChangeCheckPollTime(v string) (time.Duration, error) {
	return parsePositiveDuration("MEDIA_CHANGE_CHECK_POLL_TIME", v)
}

func parseMediaFullResyncTime(v string) (time.Duration, error) {
	return parsePositiveDuration("MEDIA_FULL_RESYNC_TIME", v)
}

func parsePositiveDuration(key, v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, fmt.Errorf("%s must not be empty", key)
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s %q (must be a positive Go duration)", key, v)
	}
	return d, nil
}

func parseSources(v string) ([]torbox.DownloadType, error) {
	if strings.TrimSpace(v) == "" {
		return nil, fmt.Errorf("TORBOX_SOURCES must not be empty (valid: torrent, usenet, webdl)")
	}
	seen := map[torbox.DownloadType]bool{}
	var out []torbox.DownloadType
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		dt, ok := torbox.ParseDownloadType(s)
		if !ok {
			return nil, fmt.Errorf("invalid TORBOX_SOURCES value %q (valid: torrent, usenet, webdl)", s)
		}
		if !seen[dt] {
			seen[dt] = true
			out = append(out, dt)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("TORBOX_SOURCES must not be empty (valid: torrent, usenet, webdl)")
	}
	return out, nil
}

func parseBoolEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parsePort(v string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid WEBAPP_PORT %q (must be an integer from 1 to 65535)", v)
	}
	return port, nil
}

func MaskAPIKey(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
