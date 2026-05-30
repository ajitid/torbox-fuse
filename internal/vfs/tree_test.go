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

func TestBuildTreeWithPlexRenamedDuplicates(t *testing.T) {
	recs := []media.FileRecord{
		{Key: "k1", MIMEType: "video/x-matroska", FolderName: "Dune.2021.1080p.BluRay.x264.mkv", FileName: "Dune.2021.1080p.BluRay.x264.mkv", OriginalPath: "Dune.2021.1080p.BluRay.x264.mkv", MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFileName: "Dune (2021).mkv"},
		{Key: "k2", MIMEType: "video/x-matroska", FolderName: "Dune.2021.2160p.WEB-DL.HEVC.mkv", FileName: "Dune.2021.2160p.WEB-DL.HEVC.mkv", OriginalPath: "Dune.2021.2160p.WEB-DL.HEVC.mkv", MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFileName: "Dune (2021).mkv"},
	}
	media.ApplyPlexVersionNaming(recs)
	tr := Build(recs)
	if !tr.IsFile("/movies/Dune (2021)/Dune (2021) - 1080p BluRay x264.mkv") || !tr.IsFile("/movies/Dune (2021)/Dune (2021) - 2160p WEB-DL HEVC.mkv") {
		t.Fatalf("missing renamed duplicate movie paths")
	}
}
