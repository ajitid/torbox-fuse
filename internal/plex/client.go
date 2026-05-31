package plex

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
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
	log     *log.Logger
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

func New(baseURL, token string, logger *log.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		log:     logger,
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

	sections, err := c.sections(ctx)
	if err != nil {
		c.printf("plex library refresh failed: %v", err)
		return
	}

	desiredPaths := []string{
		filepath.Join(mountPath, "movies"),
		filepath.Join(mountPath, "series"),
	}
	seen := make(map[string]struct{})
	matched := 0
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
		matched++
		if err := c.refresh(ctx, sectionKey, scanPath); err != nil {
			c.printf("plex library refresh failed for section %s path %q: %v", sectionKey, scanPath, err)
		}
	}
	if matched == 0 {
		c.printf("plex library refresh found no matching sections for %s", strings.Join(desiredPaths, ", "))
	}
}

func (c *Client) sections(ctx context.Context) ([]section, error) {
	u, ok := c.apiURL("/library/sections", nil)
	if !ok {
		return nil, fmt.Errorf("invalid base url %q", c.baseURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, responseError("GET /library/sections", resp)
	}
	var sections sectionsResponse
	if err := xml.NewDecoder(resp.Body).Decode(&sections); err != nil {
		return nil, err
	}
	return sections.Directories, nil
}

func (c *Client) refresh(ctx context.Context, sectionKey, scanPath string) error {
	u, ok := c.apiURL(path.Join("/library/sections", sectionKey, "refresh"), url.Values{"path": []string{scanPath}})
	if !ok {
		return fmt.Errorf("invalid base url %q", c.baseURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError("GET /library/sections/{id}/refresh", resp)
	}
	return nil
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

func (c *Client) printf(format string, args ...any) {
	if c.log != nil {
		c.log.Printf(format, args...)
	}
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

func responseError(operation string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if len(snippet) > 0 {
		return fmt.Errorf("%s returned %s: %s", operation, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return fmt.Errorf("%s returned %s", operation, resp.Status)
}
