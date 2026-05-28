package media

import (
	"regexp"
	"strings"

	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

type FileRecord struct {
	Key                     string              `json:"key"`
	ItemID                  string              `json:"item_id"`
	Type                    torbox.DownloadType `json:"type"`
	FolderName              string              `json:"folder_name"`
	FolderHash              string              `json:"folder_hash"`
	FileID                  string              `json:"file_id"`
	FileName                string              `json:"file_name"`
	FileSize                int64               `json:"file_size"`
	MIMEType                string              `json:"file_mimetype"`
	OriginalPath            string              `json:"path"`
	DownloadLink            string              `json:"download_link"`
	Extension               string              `json:"extension"`
	MetadataTitle           string              `json:"metadata_title"`
	MetadataMediaType       string              `json:"metadata_mediatype"`
	MetadataYears           *int                `json:"metadata_years,omitempty"`
	MetadataSeason          *int                `json:"metadata_season,omitempty"`
	MetadataEpisode         *int                `json:"metadata_episode,omitempty"`
	MetadataRootFolderName  string              `json:"metadata_rootfoldername"`
	MetadataFolderName      string              `json:"metadata_foldername,omitempty"`
	MetadataExtraFolderName string              `json:"metadata_extrafoldername,omitempty"`
	MetadataFileName        string              `json:"metadata_filename"`
	MetadataLink            string              `json:"metadata_link,omitempty"`
	MetadataImage           string              `json:"metadata_image,omitempty"`
	MetadataBackdrop        string              `json:"metadata_backdrop,omitempty"`
}

type Metadata struct {
	Title, MediaType                                      string
	Years                                                 *int
	Season                                                *int
	Episode                                               *int
	RootFolderName, FolderName, ExtraFolderName, FileName string
}

var videoMIME = map[string]bool{"video/x-matroska": true, "video/mp4": true, "video/quicktime": true, "video/mpeg": true, "video/x-msvideo": true, "video/webm": true}
var subtitleMIME = map[string]bool{"application/x-subrip": true, "text/vtt": true, "text/x-ass": true, "text/x-ssa": true}

func IsVideoMIME(m string) bool    { return videoMIME[strings.ToLower(strings.TrimSpace(m))] }
func IsSubtitleMIME(m string) bool { return subtitleMIME[strings.ToLower(strings.TrimSpace(m))] }
func AcceptableMIME(m string) bool { return IsVideoMIME(m) || IsSubtitleMIME(m) }

var unsafePath = regexp.MustCompile(`[\\/:*?"<>|]`)
var spaces = regexp.MustCompile(`\s+`)

func SafePathName(v string) string {
	v = unsafePath.ReplaceAllString(v, "")
	v = spaces.ReplaceAllString(v, " ")
	v = strings.TrimRight(strings.TrimSpace(v), ".")
	if v == "" {
		return "Unknown"
	}
	return v
}

func ptr(i int) *int { return &i }
