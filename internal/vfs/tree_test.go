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

func TestRecordPathShowLevelSeriesExtra(t *testing.T) {
	r := media.FileRecord{
		MetadataMediaType:       "series",
		MetadataRootFolderName:  "Example Show (2020)",
		MetadataExtraFolderName: "Other",
		MetadataFileName:        "sample.mkv",
	}
	if got, want := RecordPath(r), "/series/Example Show (2020)/Other/sample.mkv"; got != want {
		t.Fatalf("RecordPath() = %q, want %q", got, want)
	}
}

func TestBuildTreeAfterCanonicalRootCasingMergesCaseOnlySeriesRoots(t *testing.T) {
	recs := []media.FileRecord{
		{Key: "k1", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFolderName: "Season 01", MetadataFileName: "Mad Men - s01e01.mkv"},
		{Key: "k2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFolderName: "Season 05", MetadataFileName: "mad men - s05e01.mkv"},
	}
	media.ApplyCanonicalRootCasing(recs)
	tr := Build(recs)
	if tr.IsDir("/series/mad men") {
		t.Fatalf("unexpected lower-case split root")
	}
	if !tr.IsDir("/series/Mad Men/Season 01") || !tr.IsDir("/series/Mad Men/Season 05") {
		t.Fatalf("missing merged Mad Men seasons")
	}
}
