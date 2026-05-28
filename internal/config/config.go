package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey       string
	MountPath    string
	DataPath     string
	RefreshEvery time.Duration
	AllowOther   bool
	Version      string
	CachePath    string
	CacheSize    int64
	ReadAhead    int64
}

func Load() (Config, error) {
	apiKey := strings.TrimSpace(os.Getenv("TORBOX_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("TORBOX_API_KEY is required")
	}
	mountPath := envDefault("MOUNT_PATH", "./torbox")
	dataPath := envDefault("DATA_PATH", "./torbox-media-center.db")
	refresh, err := parseRefreshPreset(envDefault("MOUNT_REFRESH_TIME", "normal"))
	if err != nil {
		return Config{}, err
	}
	cacheRoot, _ := os.UserCacheDir()
	if cacheRoot == "" {
		cacheRoot = os.TempDir()
	}
	cacheSize, err := parseBytes(envDefault("CACHE_SIZE", "7GiB"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid CACHE_SIZE: %w", err)
	}
	readAhead, err := parseBytes(envDefault("READ_AHEAD", "600MiB"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid READ_AHEAD: %w", err)
	}
	return Config{
		APIKey:       apiKey,
		MountPath:    mountPath,
		DataPath:     dataPath,
		RefreshEvery: refresh,
		AllowOther:   parseBoolEnv(os.Getenv("FUSE_ALLOW_OTHER")),
		Version:      "dev",
		CachePath:    envDefault("CACHE_PATH", filepath.Join(cacheRoot, "torbox-media-center")),
		CacheSize:    cacheSize,
		ReadAhead:    readAhead,
	}, nil
}

func envDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func parseRefreshPreset(v string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "slowest":
		return 24 * time.Hour, nil
	case "very_slow":
		return 12 * time.Hour, nil
	case "slow":
		return 6 * time.Hour, nil
	case "normal", "":
		return 3 * time.Hour, nil
	case "fast":
		return 2 * time.Hour, nil
	case "ultra_fast":
		return time.Hour, nil
	case "instant":
		return 6 * time.Minute, nil
	default:
		return 0, fmt.Errorf("invalid MOUNT_REFRESH_TIME %q", v)
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

func parseBytes(v string) (int64, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	units := []struct {
		suffix string
		mul    int64
	}{
		{"tib", 1024 * 1024 * 1024 * 1024}, {"tb", 1000 * 1000 * 1000 * 1000},
		{"gib", 1024 * 1024 * 1024}, {"gb", 1000 * 1000 * 1000}, {"g", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024}, {"mb", 1000 * 1000}, {"m", 1024 * 1024},
		{"kib", 1024}, {"kb", 1000}, {"k", 1024},
		{"b", 1},
	}
	lower := strings.ToLower(s)
	mul := int64(1)
	for _, u := range units {
		if strings.HasSuffix(lower, u.suffix) {
			mul = u.mul
			s = strings.TrimSpace(s[:len(s)-len(u.suffix)])
			break
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return int64(n * float64(mul)), nil
}

func MaskAPIKey(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
