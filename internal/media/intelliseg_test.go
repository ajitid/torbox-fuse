package media

import "testing"

func TestClassifyMovie(t *testing.T) {
	m := Classify("Inception 2010 1080p", "Inception.2010.1080p.mkv", "Inception.2010.1080p.mkv")
	if m.MediaType != "movie" || m.RootFolderName != "Inception (2010)" || m.FileName != "Inception (2010).mkv" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifySeries(t *testing.T) {
	m := Classify("Show", "Show.S01E02.mkv", "Show/Season 1/Show.S01E02.mkv")
	if m.MediaType != "series" || m.Season == nil || *m.Season != 1 || m.Episode == nil || *m.Episode != 2 || m.FolderName != "Season 1" {
		t.Fatalf("unexpected: %#v", m)
	}
}

func TestClassifyMovieExtra(t *testing.T) {
	m := Classify("Film 2020", "Trailer.mkv", "Film/Trailers/Trailer.mkv")
	if m.MediaType != "movie" || m.FolderName != "Trailers" {
		t.Fatalf("unexpected: %#v", m)
	}
}
