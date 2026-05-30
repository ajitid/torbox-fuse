package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/TorBox-App/torbox-fuse/internal/cache"
	"github.com/TorBox-App/torbox-fuse/internal/config"
	"github.com/TorBox-App/torbox-fuse/internal/fusefs"
	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/plex"
	"github.com/TorBox-App/torbox-fuse/internal/refresh"
	"github.com/TorBox-App/torbox-fuse/internal/store"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	flag.Parse()
	if runtime.GOOS == "windows" {
		return errors.New("native FUSE mount is not supported on Windows")
	}
	logger := log.New(os.Stderr, "torbox-fuse: ", log.LstdFlags)
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Printf("mount=%s data=%s cache=%s refresh=%s api_key=%s allow_other=%v web=http://0.0.0.0:3939", cfg.MountPath, cfg.DataPath, cfg.CachePath, cfg.RefreshEvery, config.MaskAPIKey(cfg.APIKey), cfg.AllowOther)
	if err := ensureEmptyMountPath(cfg.MountPath); err != nil {
		return err
	}
	st, err := store.Open(cfg.DataPath)
	if err != nil {
		return err
	}
	defer st.Close()
	bc, err := cache.New(cfg.CachePath, cfg.CacheSize, cfg.ReadAhead)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	client := torbox.New(cfg.APIKey, cfg.Version)
	plexClient := plex.New(cfg.PlexBaseURL, cfg.PlexAccessToken)
	mgr := refresh.New(client, st, logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger.Printf("initial refresh starting")
	records, err := mgr.Run(ctx)
	if err != nil {
		return fmt.Errorf("initial refresh failed: %w", err)
	}
	root := fusefs.New(records, client, bc)
	server, err := root.Mount(cfg.MountPath, cfg.AllowOther, logger)
	if err != nil {
		return fmt.Errorf("mount failed: %w", err)
	}
	logger.Printf("mounted on %s with %d files", cfg.MountPath, len(records))
	plexClient.RefreshMountPaths(ctx, cfg.MountPath)
	refreshOnce := func(ctx context.Context, reason string) (int, error) {
		logger.Printf("%s refresh starting", reason)
		recs, err := mgr.Run(ctx)
		if err != nil {
			logger.Printf("%s refresh failed: %v", reason, err)
			return 0, err
		}
		root.Swap(recs)
		plexClient.RefreshMountPaths(ctx, cfg.MountPath)
		logger.Printf("%s refresh applied: %d files", reason, len(recs))
		return len(recs), nil
	}
	listFiles := func(ctx context.Context) ([]media.FileRecord, error) {
		return st.All(ctx)
	}
	addTorrent := func(ctx context.Context, magnet string) error {
		if _, err := client.CreateTorrent(ctx, magnet); err != nil {
			return err
		}
		_, err := refreshOnce(ctx, "add torrent")
		return err
	}
	deleteTorrent := func(ctx context.Context, id string) error {
		if err := client.DeleteTorrent(ctx, id); err != nil {
			return err
		}
		_, err := refreshOnce(ctx, "delete torrent")
		return err
	}
	if _, err := startWebServer(ctx, logger, refreshOnce, listFiles, addTorrent, deleteTorrent); err != nil {
		return fmt.Errorf("start web server: %w", err)
	}
	go func() {
		ticker := time.NewTicker(cfg.RefreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = refreshOnce(ctx, "scheduled")
			}
		}
	}()
	<-ctx.Done()
	logger.Printf("unmounting %s", cfg.MountPath)
	if err := server.Unmount(); err != nil {
		logger.Printf("normal unmount failed: %v", err)
		if lazyErr := lazyUnmount(cfg.MountPath); lazyErr != nil {
			logger.Printf("lazy unmount failed: %v", lazyErr)
		} else {
			logger.Printf("lazy unmount succeeded")
		}
	}
	return nil
}

func ensureEmptyMountPath(p string) error {
	if err := os.MkdirAll(p, 0755); err != nil {
		if lazyErr := lazyUnmount(p); lazyErr != nil {
			return err
		}
		if retryErr := os.MkdirAll(p, 0755); retryErr != nil {
			return retryErr
		}
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		if lazyErr := lazyUnmount(p); lazyErr != nil {
			return err
		}
		entries, err = os.ReadDir(p)
		if err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		return fmt.Errorf("mount path must be empty: %s", p)
	}
	return nil
}

func lazyUnmount(p string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("lazy FUSE unmount is only implemented on Linux")
	}
	cmd := exec.Command("fusermount3", "-uz", p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fusermount3 -uz: %w: %s", err, string(out))
	}
	return nil
}
