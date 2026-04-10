package notion

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	token   string
	version string
	debug   bool
	httpc   *http.Client
	retries int
	baseURL string
}

var (
	defaultTransport http.RoundTripper
	transportMu      sync.RWMutex
)

// SetDefaultTransport configures the HTTP transport used by new clients when none
// is explicitly provided. Primarily used by tests to inject an in-memory mock.
func SetDefaultTransport(rt http.RoundTripper) {
	transportMu.Lock()
	defer transportMu.Unlock()
	defaultTransport = rt
}

func NewClient(token, version string, debug bool) *Client {
	transportMu.RLock()
	defer transportMu.RUnlock()
	httpc := &http.Client{Timeout: 30 * time.Second}
	if defaultTransport != nil {
		httpc.Transport = defaultTransport
	}
	return &Client{
		token:   token,
		version: version,
		debug:   debug,
		httpc:   httpc,
		retries: 3,
		baseURL: "https://api.notion.com",
	}
}

// SetBaseURL overrides the default Notion API base URL (useful for tests).
func (c *Client) SetBaseURL(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	c.baseURL = strings.TrimRight(raw, "/")
}

// SetTransport overrides the HTTP transport (useful for mocks in tests).
func (c *Client) SetTransport(rt http.RoundTripper) {
	if rt == nil {
		return
	}
	c.httpc.Transport = rt
}

func (c *Client) endpoint(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path
}

func (c *Client) do(method, url string, body []byte) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	var lastBody []byte
	var lastCode int
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		req, err := http.NewRequest(method, url, r)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Notion-Version", c.version)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpc.Do(req)
		if err != nil {
			lastErr = err
			lastCode = 0
		} else {
			lastErr = nil
			lastCode = resp.StatusCode
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastBody = b
			if c.debug {
				fmt.Printf("DEBUG %s %s -> %d\n", method, url, resp.StatusCode)
			}
			// Success
			if resp.StatusCode/100 == 2 {
				return b, resp.StatusCode, nil
			}
			// Retry on 429 or 5xx
			if resp.StatusCode == 429 || resp.StatusCode/100 == 5 {
				// Exponential backoff with jitter
				time.Sleep(time.Duration(250*(1<<attempt)) * time.Millisecond)
				continue
			}
			// Non-retryable
			return b, resp.StatusCode, nil
		}
		// Network error retry
		time.Sleep(time.Duration(250*(1<<attempt)) * time.Millisecond)
	}
	return lastBody, lastCode, lastErr
}

func (c *Client) GetDatabase(id string) ([]byte, int, error) {
	return c.do(http.MethodGet, c.endpoint(fmt.Sprintf("/v1/databases/%s", id)), nil)
}

func (c *Client) QueryDatabase(id string, payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPost, c.endpoint(fmt.Sprintf("/v1/databases/%s/query", id)), payload)
}

func (c *Client) GetPage(id string) ([]byte, int, error) {
	return c.do(http.MethodGet, c.endpoint(fmt.Sprintf("/v1/pages/%s", id)), nil)
}

func (c *Client) CreatePage(payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPost, c.endpoint("/v1/pages"), payload)
}

func (c *Client) UpdatePage(id string, payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPatch, c.endpoint(fmt.Sprintf("/v1/pages/%s", id)), payload)
}

// AppendBlockChildren appends child blocks to the given block (page id works here).
func (c *Client) AppendBlockChildren(id string, payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPatch, c.endpoint(fmt.Sprintf("/v1/blocks/%s/children", id)), payload)
}

func (c *Client) CreateDatabase(payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPost, c.endpoint("/v1/databases"), payload)
}

func (c *Client) UpdateDatabase(id string, payload []byte) ([]byte, int, error) {
	return c.do(http.MethodPatch, c.endpoint(fmt.Sprintf("/v1/databases/%s", id)), payload)
}
