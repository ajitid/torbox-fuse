package vfs

import (
	"testing"

	"github.com/TorBox-App/torbox-fuse/internal/media"
)

func TestBuildTree(t *testing.T) {
	recs := []media.FileRecord{{Key: "k", FileSize: 42, MetadataMediaType: "movie", MetadataRootFolderName: "Movie (2020)", MetadataFileName: "Movie (2020).mkv"}}
	tr := Build(recs)
	if !tr.IsDir("/movies") || !tr.IsFile("/movies/Movie (2020)/Movie (2020).mkv") {
		t.Fatalf("missing expected paths")
	}
	ents := tr.ListDir("/")
	if len(ents) != 3 || ents[0].Name != "movies" || ents[1].Name != "series" || ents[2].Name != "unknown" {
		t.Fatalf("unexpected root entries: %#v", ents)
	}
}
