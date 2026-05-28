package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/TorBox-App/torbox-rclone/internal/cache"
	"github.com/TorBox-App/torbox-rclone/internal/config"
	"github.com/TorBox-App/torbox-rclone/internal/fusefs"
	"github.com/TorBox-App/torbox-rclone/internal/refresh"
	"github.com/TorBox-App/torbox-rclone/internal/store"
	"github.com/TorBox-App/torbox-rclone/internal/torbox"
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
	logger := log.New(os.Stderr, "torbox-media-center: ", log.LstdFlags)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Printf("mount=%s data=%s cache=%s cache_size=%d read_ahead=%d refresh=%s api_key=%s allow_other=%v", cfg.MountPath, cfg.DataPath, cfg.CachePath, cfg.CacheSize, cfg.ReadAhead, cfg.RefreshEvery, config.MaskAPIKey(cfg.APIKey), cfg.AllowOther)
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
	go func() {
		ticker := time.NewTicker(cfg.RefreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recs, err := mgr.Run(ctx)
				if err != nil {
					logger.Printf("refresh failed: %v", err)
					continue
				}
				root.Swap(recs)
				logger.Printf("refresh applied: %d files", len(recs))
			}
		}
	}()
	<-ctx.Done()
	logger.Printf("unmounting %s", cfg.MountPath)
	if err := server.Unmount(); err != nil {
		logger.Printf("unmount failed: %v", err)
	}
	return nil
}

func ensureEmptyMountPath(p string) error {
	if err := os.MkdirAll(p, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("mount path must be empty: %s", p)
	}
	return nil
}
