package media

import (
	"strings"
	"testing"
)

func TestClassifyMovie(t *testing.T) {
	m := Classify("Inception 2010 1080p", "Inception.2010.1080p.mkv", "Inception.2010.1080p.mkv", 0)
	if m.MediaType != "movie" || m.RootFolderName != "Inception (2010)" || m.FileName != "Inception (2010).mkv" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifySeries(t *testing.T) {
	m := Classify("Show", "Show.S01E02.mkv", "Show/Season 1/Show.S01E02.mkv", 0)
	if m.MediaType != "series" || m.Season == nil || *m.Season != 1 || m.Episode == nil || *m.Episode != 2 || m.FolderName != "Season 01" || m.FileName != "Show - s01e02.mkv" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifyMovieExtra(t *testing.T) {
	m := Classify("Film 2020", "Trailer.mkv", "Film/Trailers/Trailer.mkv", 0)
	if m.MediaType != "movie" || m.FolderName != "Trailers" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifyRealMovieArgo(t *testing.T) {
	m := Classify(
		"Argo (2012) 1080p DV HDR Bluray AV1 EAC3",
		"Argo.2012.1080p.DV.HDR.Bluray.AV1.EAC3.MultiSub.mkv",
		"Argo (2012) 1080p DV HDR Bluray AV1 EAC3/Argo.2012.1080p.DV.HDR.Bluray.AV1.EAC3.MultiSub.mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:          "Argo",
		MediaType:      "movie",
		RootFolderName: "Argo (2012)",
		FileName:       "Argo (2012).mkv",
	})
	assertPtr(t, m.Years, 2012)
}

func TestClassifyRealHomelandEpisode(t *testing.T) {
	m := Classify(
		"Homeland (2011) Season 4 S04 + Extras (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence)",
		"Homeland (2011) - S04E12 - Long Time Coming (1080p BluRay x265 Silence).mkv",
		"Homeland (2011) Season 4 S04 + Extras (1080p BluRay x265 HEVC 10bit AAC 5.1 Silence)/Homeland (2011) - S04E12 - Long Time Coming (1080p BluRay x265 Silence).mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:          "Homeland",
		MediaType:      "series",
		RootFolderName: "Homeland (2011)",
		FolderName:     "Season 04",
		FileName:       "Homeland (2011) - s04e12.mkv",
	})
	assertPtr(t, m.Years, 2011)
	assertPtr(t, m.Season, 4)
	assertPtr(t, m.Episode, 12)
}

func TestClassifyRealForAllMankindEpisode(t *testing.T) {
	m := Classify(
		"For All Mankind (2019) S02 (1080p BluRay x265 10bit EAC3 5.1 Silence) [QxR]",
		"For All Mankind (2019) - S02E01 - Every Little Thing (1080p BluRay x265 Silence).mkv",
		"For All Mankind (2019) S02 (1080p BluRay x265 10bit EAC3 5.1 Silence) [QxR]/For All Mankind (2019) - S02E01 - Every Little Thing (1080p BluRay x265 Silence).mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:          "For All Mankind",
		MediaType:      "series",
		RootFolderName: "For All Mankind (2019)",
		FolderName:     "Season 02",
		FileName:       "For All Mankind (2019) - s02e01.mkv",
	})
	assertPtr(t, m.Years, 2019)
	assertPtr(t, m.Season, 2)
	assertPtr(t, m.Episode, 1)
}

func TestClassifySeriesUsesFirstYearOfYearRange(t *testing.T) {
	m := Classify(
		"Lost (2004-2010) Season 1",
		"Lost.S01E01.2004-2010.1080p.BluRay.mkv",
		"Lost (2004-2010) Season 1/Lost.S01E01.2004-2010.1080p.BluRay.mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:          "Lost",
		MediaType:      "series",
		RootFolderName: "Lost (2004)",
		FolderName:     "Season 01",
		FileName:       "Lost (2004) - s01e01.mkv",
	})
	assertPtr(t, m.Years, 2004)
}

func TestClassifyTedLassoFeaturette(t *testing.T) {
	m := Classify(
		"Ted Lasso (2020) S01 (1080p BluRay x265 10bit EAC3 5.1 Ghost)",
		"Season 1 - Extra Time with Coach Lasso - NBC Sports.mkv",
		"Ted Lasso (2020) S01 (1080p BluRay x265 10bit EAC3 5.1 Ghost)/Featurettes/Season 1 - Extra Time with Coach Lasso - NBC Sports.mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:           "Ted Lasso",
		MediaType:       "series",
		RootFolderName:  "Ted Lasso (2020)",
		FolderName:      "Season 01",
		ExtraFolderName: "Featurettes",
		FileName:        "Extra Time with Coach Lasso - NBC Sports.mkv",
	})
	assertPtr(t, m.Years, 2020)
	assertPtr(t, m.Season, 1)
	assertNoPtr(t, m.Episode)
}

func TestClassifyCrazyStupidLoveMovieAndFeaturette(t *testing.T) {
	movie := Classify(
		"Crazy, Stupid, Love. (2011) 1080p BluRay x265",
		"Crazy, Stupid, Love.2011.1080p.BluRay.x265.mkv",
		"Crazy, Stupid, Love. (2011) 1080p BluRay x265/Crazy, Stupid, Love.2011.1080p.BluRay.x265.mkv",
		0,
	)
	assertMetadata(t, movie, Metadata{
		Title:          "Crazy, Stupid, Love",
		MediaType:      "movie",
		RootFolderName: "Crazy, Stupid, Love (2011)",
		FileName:       "Crazy, Stupid, Love (2011).mkv",
	})
	assertPtr(t, movie.Years, 2011)

	extra := Classify(
		"Crazy, Stupid, Love. (2011) 1080p BluRay x265",
		"Deleted Scenes.mkv",
		"Crazy, Stupid, Love. (2011) 1080p BluRay x265/Featurettes/Deleted Scenes.mkv",
		0,
	)
	assertMetadata(t, extra, Metadata{
		Title:          "Crazy, Stupid, Love",
		MediaType:      "movie",
		RootFolderName: "Crazy, Stupid, Love (2011)",
		FolderName:     "Featurettes",
		FileName:       "Deleted Scenes.mkv",
	})
	assertPtr(t, extra.Years, 2011)
}

func TestClassify1917WithLanguageAndAudioTags(t *testing.T) {
	m := Classify(
		"1917 (2019) 2160p H265 10 bit DV HDR10+ ita eng AC-3 5.1 sub ita eng Licdom.mkv",
		"1917 (2019) 2160p H265 10 bit DV HDR10+ ita eng AC-3 5.1 sub ita eng Licdom.mkv",
		"1917 (2019) 2160p H265 10 bit DV HDR10+ ita eng AC-3 5.1 sub ita eng Licdom/1917 (2019) 2160p H265 10 bit DV HDR10+ ita eng AC-3 5.1 sub ita eng Licdom.mkv",
		0,
	)
	assertMetadata(t, m, Metadata{
		Title:          "1917",
		MediaType:      "movie",
		RootFolderName: "1917 (2019)",
		FileName:       "1917 (2019).mkv",
	})
	assertPtr(t, m.Years, 2019)
}

func TestClassifyMovieEdition(t *testing.T) {
	m := Classify("Blade Runner 1982 Directors Cut 1080p", "Blade.Runner.1982.Directors.Cut.mkv", "Blade.Runner.1982.Directors.Cut.mkv", 0)
	if m.MediaType != "movie" || m.RootFolderName != "Blade Runner (1982) {edition-Director's Cut}" || m.FileName != "Blade Runner (1982) {edition-Director's Cut}.mkv" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifySeriesNoMovieEdition(t *testing.T) {
	m := Classify("Some Show Extended S01E01 1080p", "Some.Show.S01E01.Extended.mkv", "Some.Show.S01E01.Extended.mkv", 0)
	if m.MediaType != "series" || strings.Contains(m.RootFolderName, "{edition-") || strings.Contains(m.FileName, "{edition-") {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifySmallMovieSample(t *testing.T) {
	m := Classify("Match Point (2005) 1080p", "SAMPLE-720p.mkv", "Match Point (2005)/Extras/SAMPLE-720p.mkv", sampleMaxBytes-1)
	assertMetadata(t, m, Metadata{
		Title:          "Match Point",
		MediaType:      "movie",
		RootFolderName: "Match Point (2005)",
		FolderName:     "Other",
		FileName:       "SAMPLE-720p.mkv",
	})
	assertPtr(t, m.Years, 2005)
	assertNoPtr(t, m.Season)
	assertNoPtr(t, m.Episode)
}

func TestClassifySampleAtSizeLimitNormally(t *testing.T) {
	m := Classify("Match Point (2005) 1080p", "sample.mkv", "Match Point (2005)/sample.mkv", sampleMaxBytes)
	if m.MediaType != "movie" || m.FolderName == "Other" || m.ExtraFolderName != "" {
		t.Fatalf("sample at size limit was classified as an extra: %#v", m)
	}
}

func TestClassifySmallNonPrefixSampleNormally(t *testing.T) {
	m := Classify("Match Point (2005) 1080p", "Match-Point-sample.mkv", "Match Point (2005)/Match-Point-sample.mkv", sampleMaxBytes-1)
	if m.MediaType != "movie" || m.FolderName == "Other" || m.ExtraFolderName != "" {
		t.Fatalf("non-prefix sample was classified as an extra: %#v", m)
	}
}

func TestClassifySmallSeriesSampleWithSeason(t *testing.T) {
	m := Classify("Example Show (2020) Season 2", "Sample 2.mp4", "Example Show (2020)/Season 2/Sample 2.mp4", sampleMaxBytes-1)
	assertMetadata(t, m, Metadata{
		Title:           "Example Show",
		MediaType:       "series",
		RootFolderName:  "Example Show (2020)",
		FolderName:      "Season 02",
		ExtraFolderName: "Other",
		FileName:        "Sample 2.mp4",
	})
	assertPtr(t, m.Years, 2020)
	assertPtr(t, m.Season, 2)
	assertNoPtr(t, m.Episode)
}

func TestClassifySmallSeriesSampleWithoutSeason(t *testing.T) {
	m := Classify("Example Show (2020)", "sample.mkv", "media/tv/Example Show (2020)/sample.mkv", sampleMaxBytes-1)
	assertMetadata(t, m, Metadata{
		Title:           "Example Show",
		MediaType:       "series",
		RootFolderName:  "Example Show (2020)",
		ExtraFolderName: "Other",
		FileName:        "sample.mkv",
	})
	assertPtr(t, m.Years, 2020)
	assertNoPtr(t, m.Season)
	assertNoPtr(t, m.Episode)
}

func TestParseTorrentNameYearLikeTitle1917(t *testing.T) {
	p := parseTorrentName("1917.2019.1080p.BluRay.x265.mkv")
	if p.Title != "1917" {
		t.Fatalf("title = %q, want 1917", p.Title)
	}
	assertPtr(t, p.Year, 2019)

	p = parseTorrentName("1917.1080p.BluRay.x265.mkv")
	if p.Title != "1917" {
		t.Fatalf("title = %q, want 1917", p.Title)
	}
	assertNoPtr(t, p.Year)
}

func assertMetadata(t *testing.T, got, want Metadata) {
	t.Helper()
	if got.Title != want.Title || got.MediaType != want.MediaType || got.RootFolderName != want.RootFolderName || got.FolderName != want.FolderName || got.ExtraFolderName != want.ExtraFolderName || got.FileName != want.FileName {
		t.Fatalf("unexpected metadata:\n got: %#v\nwant: %#v", got, want)
	}
}

func assertPtr(t *testing.T, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("pointer = %v, want %d", got, want)
	}
}

func assertNoPtr(t *testing.T, got *int) {
	t.Helper()
	if got != nil {
		t.Fatalf("pointer = %d, want nil", *got)
	}
}
