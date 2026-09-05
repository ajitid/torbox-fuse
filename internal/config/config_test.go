package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMediaChangeCheckPollTime(t *testing.T) {
	tests := []struct {
		name    string
		value   *string
		want    time.Duration
		wantErr string
	}{
		{name: "default", want: 15 * time.Second},
		{name: "seconds", value: ptr("15s"), want: 15 * time.Second},
		{name: "compound", value: ptr("2h5s"), want: 2*time.Hour + 5*time.Second},
		{name: "empty", value: ptr(""), wantErr: "MEDIA_CHANGE_CHECK_POLL_TIME"},
		{name: "malformed", value: ptr("later"), wantErr: "MEDIA_CHANGE_CHECK_POLL_TIME"},
		{name: "zero", value: ptr("0s"), wantErr: "MEDIA_CHANGE_CHECK_POLL_TIME"},
		{name: "negative", value: ptr("-1s"), wantErr: "MEDIA_CHANGE_CHECK_POLL_TIME"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TORBOX_API_KEY", "key")
			t.Setenv("MEDIA_FULL_RESYNC_TIME", "3h")
			if test.value != nil {
				t.Setenv("MEDIA_CHANGE_CHECK_POLL_TIME", *test.value)
			} else {
				t.Setenv("MEDIA_CHANGE_CHECK_POLL_TIME", "")
				// An empty value is distinct from an unset one.
				if err := os.Unsetenv("MEDIA_CHANGE_CHECK_POLL_TIME"); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Load() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MediaChangeCheckPollTime != test.want {
				t.Fatalf("poll time = %s, want %s", cfg.MediaChangeCheckPollTime, test.want)
			}
		})
	}
}

func TestMediaFullResyncTime(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("TORBOX_API_KEY", "key")
		t.Setenv("MEDIA_CHANGE_CHECK_POLL_TIME", "15s")
		t.Setenv("MEDIA_FULL_RESYNC_TIME", "")
		if err := os.Unsetenv("MEDIA_FULL_RESYNC_TIME"); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.MediaFullResyncTime != 24*time.Hour {
			t.Fatalf("resync time = %s, want %s", cfg.MediaFullResyncTime, 24*time.Hour)
		}
	})
	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "3h", want: 3 * time.Hour},
		{value: "2h5s", want: 2*time.Hour + 5*time.Second},
		{value: "0s", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("TORBOX_API_KEY", "key")
			t.Setenv("MEDIA_CHANGE_CHECK_POLL_TIME", "15s")
			t.Setenv("MEDIA_FULL_RESYNC_TIME", test.value)
			cfg, err := Load()
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "MEDIA_FULL_RESYNC_TIME") {
					t.Fatalf("Load() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MediaFullResyncTime != test.want {
				t.Fatalf("resync time = %s, want %s", cfg.MediaFullResyncTime, test.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }
