package jellyfin

import (
	"bytes"
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

type mediaUpdatedRequest struct {
	Updates []mediaUpdate `json:"Updates"`
}

type mediaUpdate struct {
	Path       string `json:"Path"`
	UpdateType string `json:"UpdateType"`
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

	reqBody := mediaUpdatedRequest{Updates: []mediaUpdate{
		{Path: filepath.Join(mountPath, "movies"), UpdateType: "Modified"},
		{Path: filepath.Join(mountPath, "series"), UpdateType: "Modified"},
	}}
	if err := c.postMediaUpdated(ctx, reqBody); err != nil {
		c.printf("jellyfin media update notify failed: %v", err)
	}
}

func (c *Client) postMediaUpdated(ctx context.Context, body mediaUpdatedRequest) error {
	u, ok := c.apiURL("/Library/Media/Updated")
	if !ok {
		return fmt.Errorf("invalid base url %q", c.baseURL)
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Token", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if len(snippet) > 0 {
			return fmt.Errorf("POST /Library/Media/Updated returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
		}
		return fmt.Errorf("POST /Library/Media/Updated returned %s", resp.Status)
	}
	return nil
}

func (c *Client) apiURL(apiPath string) (string, bool) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", false
	}
	u.Path = path.Join(u.Path, apiPath)
	return u.String(), true
}

func (c *Client) printf(format string, args ...any) {
	if c.log != nil {
		c.log.Printf(format, args...)
	}
}
