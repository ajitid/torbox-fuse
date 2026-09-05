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
	"github.com/TorBox-App/torbox-fuse/internal/jellyfin"
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
	logger.Printf("mount=%s data=%s cache=%s media_change_check_poll_time=%s media_full_resync_time=%s api_key=%s allow_other=%v sources=%v web=http://0.0.0.0:%d", cfg.MountPath, cfg.DataPath, cfg.CachePath, cfg.MediaChangeCheckPollTime, cfg.MediaFullResyncTime, config.MaskAPIKey(cfg.APIKey), cfg.AllowOther, cfg.Sources, cfg.WebAppPort)
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
	plexClient := plex.New(cfg.PlexBaseURL, cfg.PlexAccessToken, logger)
	jellyfinClient := jellyfin.New(cfg.JellyfinBaseURL, cfg.JellyfinAPIKey, logger)
	mgr := refresh.New(client, st, logger, cfg.Sources)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger.Printf("initial refresh starting")
	initial, err := mgr.Run(ctx)
	if err != nil {
		return fmt.Errorf("initial refresh failed: %w", err)
	}
	records := initial.Records
	root := fusefs.New(records, client, bc)
	server, err := root.Mount(cfg.MountPath, cfg.AllowOther, logger)
	if err != nil {
		return fmt.Errorf("mount failed: %w", err)
	}
	logger.Printf("mounted on %s with %d files", cfg.MountPath, len(records))
	plexClient.RefreshMountPaths(ctx, cfg.MountPath)
	jellyfinClient.NotifyMountPaths(ctx, cfg.MountPath)
	refreshOnce := func(ctx context.Context, reason string, notify bool) (int, error) {
		logger.Printf("%s refresh starting", reason)
		result, err := mgr.Run(ctx)
		if err != nil {
			logger.Printf("%s refresh failed: %v", reason, err)
			return 0, err
		}
		root.Swap(result.Records)
		if notify || result.VisibleMediaChanged {
			plexClient.RefreshMountPaths(ctx, cfg.MountPath)
			jellyfinClient.NotifyMountPaths(ctx, cfg.MountPath)
		}
		logger.Printf("%s refresh applied: %d files", reason, len(result.Records))
		return len(result.Records), nil
	}
	manualRefresh := func(ctx context.Context, reason string) (int, error) {
		return refreshOnce(ctx, reason, true)
	}
	listFiles := func(ctx context.Context) ([]media.FileRecord, error) {
		return st.All(ctx)
	}
	addTorrent := func(ctx context.Context, magnet string) error {
		if _, err := client.CreateTorrent(ctx, magnet); err != nil {
			return err
		}
		_, err := manualRefresh(ctx, "add torrent")
		return err
	}
	deleteTorrent := func(ctx context.Context, id string) error {
		if err := client.DeleteTorrent(ctx, id); err != nil {
			return err
		}
		_, err := manualRefresh(ctx, "delete torrent")
		return err
	}
	if _, err := startWebServer(ctx, logger, cfg.WebAppPort, manualRefresh, listFiles, addTorrent, deleteTorrent); err != nil {
		return fmt.Errorf("start web server: %w", err)
	}
	go func() {
		pollTicker := time.NewTicker(cfg.MediaChangeCheckPollTime)
		resyncTicker := time.NewTicker(cfg.MediaFullResyncTime)
		defer pollTicker.Stop()
		defer resyncTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-pollTicker.C:
				changed, err := mgr.Poll(ctx)
				if err != nil {
					logger.Printf("scheduled media change check failed: %v", err)
					continue
				}
				if !changed {
					continue
				}
				logger.Printf("scheduled media change detected")
				_, _ = refreshOnce(ctx, "scheduled", false)
			case <-resyncTicker.C:
				logger.Printf("scheduled authoritative resync")
				_, _ = refreshOnce(ctx, "scheduled resync", false)
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
	var cmd *exec.Cmd
	var name string
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("diskutil", "unmount", "force", p)
		name = "diskutil unmount force"
	case "linux":
		cmd = exec.Command("fusermount3", "-uz", p)
		name = "fusermount3 -uz"
	default:
		return fmt.Errorf("lazy FUSE unmount is not supported on %s", runtime.GOOS)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, string(out))
	}
	return nil
}
