package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type DownloadType string

const (
	DownloadTorrent DownloadType = "torrents"
	DownloadUsenet  DownloadType = "usenet"
	DownloadWebDL   DownloadType = "webdl"
)

func AllDownloadTypes() []DownloadType {
	return []DownloadType{DownloadTorrent, DownloadUsenet, DownloadWebDL}
}

type Item struct {
	ID     string
	Name   string
	Hash   string
	Cached bool
	Files  []RemoteFile
}
type RemoteFile struct {
	ID        string
	Name      string
	ShortName string
	Size      int64
	MIMEType  string
}

type Client struct {
	apiKey    string
	http      *http.Client
	baseURL   string
	userAgent string
}

func New(apiKey, version string) *Client {
	return &Client{apiKey: apiKey, http: &http.Client{Timeout: 60 * time.Second}, baseURL: "https://api.torbox.app/v1/api", userAgent: "torbox-fuse-go/" + version}
}
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

type apiResp struct {
	Data []rawItem `json:"data"`
}

type mutationResp struct {
	Success *bool           `json:"success"`
	Detail  string          `json:"detail"`
	Data    json.RawMessage `json:"data"`
}

type CreatedTorrent struct {
	TorrentID any    `json:"torrent_id"`
	AuthID    string `json:"auth_id"`
	Hash      string `json:"hash"`
}
type rawItem struct {
	ID     any       `json:"id"`
	Name   string    `json:"name"`
	Hash   string    `json:"hash"`
	Cached bool      `json:"cached"`
	Files  []rawFile `json:"files"`
}
type rawFile struct {
	ID        any    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Size      int64  `json:"size"`
	MIMEType  string `json:"mimetype"`
}

func (c *Client) ListDownloads(ctx context.Context, typ DownloadType) ([]Item, error) {
	const limit = 1000
	var out []Item
	for offset := 0; ; offset += limit {
		u, _ := url.Parse(c.baseURL + "/" + string(typ) + "/mylist")
		q := u.Query()
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(offset))
		q.Set("bypass_cache", "true")
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", c.userAgent)
		res, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		body, rerr := io.ReadAll(res.Body)
		res.Body.Close()
		if rerr != nil {
			return nil, rerr
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list %s offset %d: HTTP %d: %s", typ, offset, res.StatusCode, strings.TrimSpace(string(body)))
		}
		var decoded apiResp
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
		if len(decoded.Data) == 0 {
			break
		}
		for _, ri := range decoded.Data {
			it := Item{ID: stringifyID(ri.ID), Name: ri.Name, Hash: ri.Hash, Cached: ri.Cached}
			for _, rf := range ri.Files {
				it.Files = append(it.Files, RemoteFile{ID: stringifyID(rf.ID), Name: rf.Name, ShortName: rf.ShortName, Size: rf.Size, MIMEType: rf.MIMEType})
			}
			out = append(out, it)
		}
		if len(decoded.Data) < limit {
			break
		}
	}
	return out, nil
}

func (c *Client) CreateTorrent(ctx context.Context, magnet string) (CreatedTorrent, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("magnet", magnet); err != nil {
		return CreatedTorrent{}, err
	}
	if err := mw.Close(); err != nil {
		return CreatedTorrent{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents/createtorrent", &body)
	if err != nil {
		return CreatedTorrent{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resBody, err := c.doMutation(req, "create torrent")
	if err != nil {
		return CreatedTorrent{}, err
	}
	var out CreatedTorrent
	if len(resBody) > 0 && string(resBody) != "null" {
		_ = json.Unmarshal(resBody, &out)
	}
	return out, nil
}

func (c *Client) DeleteTorrent(ctx context.Context, torrentID string) error {
	body, err := json.Marshal(map[string]any{"operation": "delete", "torrent_id": torrentID, "all": false})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents/controltorrent", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/json")
	_, err = c.doMutation(req, "delete torrent")
	return err
}

func (c *Client) doMutation(req *http.Request, op string) (json.RawMessage, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	body, rerr := io.ReadAll(res.Body)
	res.Body.Close()
	if rerr != nil {
		return nil, rerr
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", op, res.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.TrimSpace(string(body)) == "" {
		return nil, nil
	}
	var decoded mutationResp
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if decoded.Success != nil && !*decoded.Success {
		if decoded.Detail == "" {
			decoded.Detail = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("%s: %s", op, decoded.Detail)
	}
	return decoded.Data, nil
}

func (c *Client) PermanentDownloadURL(typ DownloadType, item Item, file RemoteFile) string {
	idParam := map[DownloadType]string{DownloadTorrent: "torrent_id", DownloadUsenet: "usenet_id", DownloadWebDL: "web_id"}[typ]
	u, _ := url.Parse(c.baseURL + "/" + string(typ) + "/requestdl")
	q := u.Query()
	q.Set("token", c.apiKey)
	q.Set(idParam, item.ID)
	q.Set("file_id", file.ID)
	q.Set("redirect", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) ResolveDownloadURL(ctx context.Context, permanentURL string) (string, error) {
	client := *c.http
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, permanentURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		loc := res.Header.Get("Location")
		if loc == "" {
			return "", fmt.Errorf("redirect had no Location")
		}
		return res.Request.URL.ResolveReference(mustParse(loc)).String(), nil
	}
	if res.StatusCode == http.StatusOK {
		return res.Request.URL.String(), nil
	}
	return "", fmt.Errorf("resolve download URL: HTTP %d", res.StatusCode)
}

func (c *Client) ReadRange(ctx context.Context, u string, off int64, size int) ([]byte, int, error) {
	if size <= 0 {
		return nil, http.StatusOK, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(size)-1))
	req.Header.Set("User-Agent", c.userAgent)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, int64(size)+1))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if res.StatusCode != http.StatusPartialContent && !(off == 0 && res.StatusCode == http.StatusOK && len(b) <= size) {
		return nil, res.StatusCode, fmt.Errorf("range read: HTTP %d", res.StatusCode)
	}
	if len(b) > size {
		b = b[:size]
	}
	return b, res.StatusCode, nil
}

func stringifyID(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case json.Number:
		return x.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
func mustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		return &url.URL{Path: s}
	}
	return u
}
