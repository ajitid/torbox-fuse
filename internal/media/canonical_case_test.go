package media

import "testing"

func TestApplyCanonicalRootCasingPrefersMixedCase(t *testing.T) {
	recs := []FileRecord{
		{Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFolderName: "Season 05", MetadataFileName: "mad men - s05e01.mkv"},
		{Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "MAD MEN", MetadataFolderName: "Season 02", MetadataFileName: "MAD MEN - s02e01.mkv"},
		{Key: "3", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFolderName: "Season 01", MetadataFileName: "Mad Men - s01e01.mkv"},
	}
	ApplyCanonicalRootCasing(recs)
	for _, r := range recs {
		if r.MetadataRootFolderName != "Mad Men" {
			t.Fatalf("root = %q, want Mad Men", r.MetadataRootFolderName)
		}
	}
}

func TestApplyCanonicalRootCasingPrefersAllCapsOverLower(t *testing.T) {
	recs := []FileRecord{
		{Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "s.t.a.l.k.e.r.", MetadataFileName: "e1.mkv"},
		{Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "S.T.A.L.K.E.R.", MetadataFileName: "e2.mkv"},
	}
	ApplyCanonicalRootCasing(recs)
	for _, r := range recs {
		if r.MetadataRootFolderName != "S.T.A.L.K.E.R." {
			t.Fatalf("root = %q, want S.T.A.L.K.E.R.", r.MetadataRootFolderName)
		}
	}
}

func TestApplyCanonicalRootCasingDoesNotCrossMediaTypes(t *testing.T) {
	recs := []FileRecord{
		{Key: "1", MetadataMediaType: "movie", MetadataRootFolderName: "Mad Men", MetadataFileName: "Mad Men.mkv"},
		{Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFileName: "mad men - s01e01.mkv"},
	}
	ApplyCanonicalRootCasing(recs)
	if recs[0].MetadataRootFolderName != "Mad Men" || recs[1].MetadataRootFolderName != "mad men" {
		t.Fatalf("unexpected roots: %#v", recs)
	}
}

func TestApplyCanonicalRootCasingOnlyCaseInsensitiveExactMatches(t *testing.T) {
	recs := []FileRecord{
		{Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFileName: "a.mkv"},
		{Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "Mad-Men", MetadataFileName: "b.mkv"},
	}
	ApplyCanonicalRootCasing(recs)
	if recs[0].MetadataRootFolderName != "Mad Men" || recs[1].MetadataRootFolderName != "Mad-Men" {
		t.Fatalf("unexpected roots: %#v", recs)
	}
}
