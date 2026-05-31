package jellyfin

import (
	"context"
	"encoding/json"
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
	apiKey  string
	log     *log.Logger
	http    *http.Client
}

type virtualFolder struct {
	Name      string   `json:"Name"`
	Locations []string `json:"Locations"`
	ItemID    string   `json:"ItemId"`
}

func New(baseURL, apiKey string, logger *log.Logger) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		log:     logger,
		http:    http.DefaultClient,
	}
}

func (c *Client) Enabled() bool {
	return strings.TrimSpace(c.apiKey) != ""
}

func (c *Client) NotifyMountPaths(ctx context.Context, mountPath string) {
	if !c.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	folders, err := c.virtualFolders(ctx)
	if err != nil {
		c.printf("jellyfin library refresh failed: %v", err)
		return
	}

	desiredPaths := []string{
		filepath.Join(mountPath, "movies"),
		filepath.Join(mountPath, "series"),
	}
	seen := make(map[string]struct{})
	matched := 0
	for _, refreshPath := range desiredPaths {
		for _, folder := range folders {
			if strings.TrimSpace(folder.ItemID) == "" || !hasLocation(folder, refreshPath) {
				continue
			}
			if _, exists := seen[folder.ItemID]; exists {
				continue
			}
			seen[folder.ItemID] = struct{}{}
			matched++
			if err := c.refreshItem(ctx, folder.ItemID); err != nil {
				c.printf("jellyfin library refresh failed for %q (%s): %v", folder.Name, folder.ItemID, err)
			}
		}
	}
	if matched == 0 {
		c.printf("jellyfin library refresh found no matching virtual folders for %s", strings.Join(desiredPaths, ", "))
	}
}

func (c *Client) virtualFolders(ctx context.Context) ([]virtualFolder, error) {
	u, ok := c.apiURL("/Library/VirtualFolders", nil)
	if !ok {
		return nil, fmt.Errorf("invalid base url %q", c.baseURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.setAuthHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, responseError("GET /Library/VirtualFolders", resp)
	}
	var folders []virtualFolder
	if err := json.NewDecoder(resp.Body).Decode(&folders); err != nil {
		return nil, err
	}
	return folders, nil
}

func (c *Client) refreshItem(ctx context.Context, itemID string) error {
	query := url.Values{
		"Recursive":           []string{"true"},
		"ImageRefreshMode":    []string{"Default"},
		"MetadataRefreshMode": []string{"Default"},
		"ReplaceAllImages":    []string{"false"},
		"RegenerateTrickplay": []string{"false"},
		"ReplaceAllMetadata":  []string{"false"},
	}
	u, ok := c.apiURL(path.Join("/Items", itemID, "Refresh"), query)
	if !ok {
		return fmt.Errorf("invalid base url %q", c.baseURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return responseError("POST /Items/{itemId}/Refresh", resp)
	}
	return nil
}

func (c *Client) apiURL(apiPath string, query url.Values) (string, bool) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", false
	}
	u.Path = path.Join(u.Path, apiPath)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String(), true
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("X-Emby-Token", c.apiKey)
}

func (c *Client) printf(format string, args ...any) {
	if c.log != nil {
		c.log.Printf(format, args...)
	}
}

func hasLocation(folder virtualFolder, scanPath string) bool {
	cleanScanPath := filepath.Clean(scanPath)
	for _, location := range folder.Locations {
		if filepath.Clean(location) == cleanScanPath {
			return true
		}
	}
	return false
}

func responseError(operation string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if len(snippet) > 0 {
		return fmt.Errorf("%s returned %s: %s", operation, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return fmt.Errorf("%s returned %s", operation, resp.Status)
}
