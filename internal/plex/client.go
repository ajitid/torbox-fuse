package plex

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type sectionsResponse struct {
	Directories []section `xml:"Directory"`
}

type section struct {
	Key       string     `xml:"key,attr"`
	Title     string     `xml:"title,attr"`
	Locations []location `xml:"Location"`
}

type location struct {
	Path string `xml:"path,attr"`
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    http.DefaultClient,
	}
}

func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.token) != ""
}

func (c *Client) RefreshMountPaths(ctx context.Context, mountPath string) {
	if !c.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	sections, ok := c.sections(ctx)
	if !ok {
		return
	}

	desiredPaths := []string{
		filepath.Join(mountPath, "movies"),
		filepath.Join(mountPath, "series"),
	}
	seen := make(map[string]struct{})
	for _, scanPath := range desiredPaths {
		sectionKey, ok := matchingSectionKey(sections, scanPath)
		if !ok {
			continue
		}
		pairKey := sectionKey + "\x00" + filepath.Clean(scanPath)
		if _, exists := seen[pairKey]; exists {
			continue
		}
		seen[pairKey] = struct{}{}
		c.refresh(ctx, sectionKey, scanPath)
	}
}

func (c *Client) sections(ctx context.Context) ([]section, bool) {
	u, ok := c.apiURL("/library/sections", nil)
	if !ok {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, false
	}
	var sections sectionsResponse
	if err := xml.NewDecoder(resp.Body).Decode(&sections); err != nil {
		return nil, false
	}
	return sections.Directories, true
}

func (c *Client) refresh(ctx context.Context, sectionKey, scanPath string) {
	u, ok := c.apiURL(path.Join("/library/sections", sectionKey, "refresh"), url.Values{"path": []string{scanPath}})
	if !ok {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (c *Client) apiURL(apiPath string, query url.Values) (string, bool) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", false
	}
	u.Path = path.Join(u.Path, apiPath)
	q := u.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	q.Set("X-Plex-Token", c.token)
	u.RawQuery = q.Encode()
	return u.String(), true
}

func matchingSectionKey(sections []section, scanPath string) (string, bool) {
	cleanScanPath := filepath.Clean(scanPath)
	for _, section := range sections {
		for _, location := range section.Locations {
			if filepath.Clean(location.Path) == cleanScanPath {
				return section.Key, true
			}
		}
	}
	return "", false
}
