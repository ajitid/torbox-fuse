package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TorBox-App/torbox-rclone/internal/media"
)

func TestReplaceAllRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db.bolt"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	recs := []media.FileRecord{{Key: "a", FileName: "a.mkv"}}
	if err := s.ReplaceAll(context.Background(), recs); err != nil {
		t.Fatal(err)
	}
	got, err := s.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "a" || got[0].FileName != "a.mkv" {
		t.Fatalf("unexpected %#v", got)
	}
	if err := s.ReplaceAll(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.All(context.Background())
	if len(got) != 0 {
		t.Fatalf("replace did not clear: %#v", got)
	}
}
