package media

import "testing"

func TestApplyPlexVersionNamingSingleMovieUnchanged(t *testing.T) {
	recs := []FileRecord{movieRecord("k1", "Dune.2021.1080p.mkv", "Dune (2021).mkv")}
	ApplyPlexVersionNaming(recs)
	if got := recs[0].MetadataFileName; got != "Dune (2021).mkv" {
		t.Fatalf("filename = %q", got)
	}
}

func TestApplyPlexVersionNamingDuplicateMovies(t *testing.T) {
	recs := []FileRecord{
		movieRecord("k1", "Dune.2021.1080p.BluRay.x264.mkv", "Dune (2021).mkv"),
		movieRecord("k2", "Dune.2021.2160p.WEB-DL.HEVC.mkv", "Dune (2021).mkv"),
	}
	ApplyPlexVersionNaming(recs)
	assertNames(t, recs, []string{"Dune (2021) - 1080p BluRay x264.mkv", "Dune (2021) - 2160p WEB-DL HEVC.mkv"})
}

func TestApplyPlexVersionNamingDuplicateMoviesSameTechnicalLabelUnique(t *testing.T) {
	recs := []FileRecord{
		movieRecord("k1", "Dune.A.Release.1080p.WEB-DL.mkv", "Dune (2021).mkv"),
		movieRecord("k2", "Dune.B.Release.1080p.WEB-DL.mkv", "Dune (2021).mkv"),
	}
	ApplyPlexVersionNaming(recs)
	if recs[0].MetadataFileName == recs[1].MetadataFileName {
		t.Fatalf("filenames not unique: %#v", recs)
	}
}

func TestApplyPlexVersionNamingDuplicateTVEpisodes(t *testing.T) {
	s1, e1 := 1, 1
	recs := []FileRecord{
		seriesRecord("k1", "Arcane.S01E01.1080p.WEB-DL.H264.mkv", "Arcane (2021) - s01e01.mkv", &s1, &e1),
		seriesRecord("k2", "Arcane.S01E01.2160p.WEB-DL.HEVC.mkv", "Arcane (2021) - s01e01.mkv", &s1, &e1),
	}
	ApplyPlexVersionNaming(recs)
	assertNames(t, recs, []string{"Arcane (2021) - s01e01 - 1080p WEB-DL H264.mkv", "Arcane (2021) - s01e01 - 2160p WEB-DL HEVC.mkv"})
}

func TestApplyPlexVersionNamingDifferentMovieEditionsNotGrouped(t *testing.T) {
	recs := []FileRecord{
		movieRecordWithRoot("k1", "Blade.Runner.1982.mkv", "Blade Runner (1982)", "Blade Runner (1982).mkv"),
		movieRecordWithRoot("k2", "Blade.Runner.1982.Directors.Cut.mkv", "Blade Runner (1982) {edition-Director's Cut}", "Blade Runner (1982) {edition-Director's Cut}.mkv"),
	}
	ApplyPlexVersionNaming(recs)
	assertNames(t, recs, []string{"Blade Runner (1982).mkv", "Blade Runner (1982) {edition-Director's Cut}.mkv"})
}

func movieRecord(key, file, meta string) FileRecord {
	return movieRecordWithRoot(key, file, "Dune (2021)", meta)
}

func movieRecordWithRoot(key, file, root, meta string) FileRecord {
	return FileRecord{Key: key, MIMEType: "video/x-matroska", FolderName: file, FileName: file, OriginalPath: file, MetadataMediaType: "movie", MetadataRootFolderName: root, MetadataFileName: meta}
}

func seriesRecord(key, file, meta string, season, episode *int) FileRecord {
	return FileRecord{Key: key, MIMEType: "video/x-matroska", FolderName: file, FileName: file, OriginalPath: file, MetadataMediaType: "series", MetadataRootFolderName: "Arcane (2021)", MetadataFolderName: "Season 01", MetadataSeason: season, MetadataEpisode: episode, MetadataFileName: meta}
}

func assertNames(t *testing.T, got []FileRecord, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].MetadataFileName != want[i] {
			t.Fatalf("[%d] filename = %q, want %q", i, got[i].MetadataFileName, want[i])
		}
	}
}
