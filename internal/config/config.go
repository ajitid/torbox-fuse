package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultCacheSize = 9 * 1024 * 1024 * 1024 / 2
	DefaultReadAhead = 600 * 1024 * 1024
)

type Config struct {
	APIKey          string
	MountPath       string
	DataPath        string
	RefreshEvery    time.Duration
	AllowOther      bool
	Version         string
	CachePath       string
	CacheSize       int64
	ReadAhead       int64
	PlexAccessToken string
	PlexBaseURL     string
}

func Load() (Config, error) {
	apiKey := strings.TrimSpace(os.Getenv("TORBOX_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("TORBOX_API_KEY is required")
	}
	mountPath := envDefault("MOUNT_PATH", "./torbox")
	dataPath := envDefault("DATA_PATH", "./torbox-fuse.db")
	plexAccessToken := strings.TrimSpace(os.Getenv("PLEX_ACCESS_TOKEN"))
	refresh, err := parseRefreshTime(envDefault("MOUNT_REFRESH_TIME", "3h"))
	if err != nil {
		return Config{}, err
	}
	cacheRoot, _ := os.UserCacheDir()
	if cacheRoot == "" {
		cacheRoot = os.TempDir()
	}
	return Config{
		APIKey:          apiKey,
		MountPath:       mountPath,
		DataPath:        dataPath,
		RefreshEvery:    refresh,
		AllowOther:      parseBoolEnv(os.Getenv("FUSE_ALLOW_OTHER")),
		Version:         "dev",
		CachePath:       envDefault("CACHE_PATH", filepath.Join(cacheRoot, "torbox-fuse")),
		CacheSize:       DefaultCacheSize,
		ReadAhead:       DefaultReadAhead,
		PlexAccessToken: plexAccessToken,
		PlexBaseURL:     envDefault("PLEX_BASE_URL", "http://127.0.0.1:32400"),
	}, nil
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func parseRefreshTime(v string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "24h":
		return 24 * time.Hour, nil
	case "12h":
		return 12 * time.Hour, nil
	case "6h":
		return 6 * time.Hour, nil
	case "3h", "":
		return 3 * time.Hour, nil
	case "1h":
		return time.Hour, nil
	case "6min":
		return 6 * time.Minute, nil
	default:
		return 0, fmt.Errorf("invalid MOUNT_REFRESH_TIME %q (valid values: 24h, 12h, 6h, 3h, 1h, 6min)", v)
	}
}

func parseBoolEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func MaskAPIKey(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
