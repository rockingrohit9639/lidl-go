// Package lidl is an unofficial client for the Lidl Plus API.
package lidl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultAppVersion is the Lidl Plus app version sent with every request.
const DefaultAppVersion = "17.9.3"

// ErrNotLoggedIn means there is no refresh token, or Lidl no longer accepts it.
var ErrNotLoggedIn = errors.New("lidl: not logged in")

// APIError is a non-2xx answer from Lidl.
type APIError struct {
	Status int
	URL    string
	Body   string
}

func (e *APIError) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "..."
	}
	return fmt.Sprintf("lidl: HTTP %d for %s: %s", e.Status, e.URL, body)
}

// Client talks to the Lidl Plus API for one account.
type Client struct {
	Country    string
	Language   string
	AppVersion string
	HTTP       *http.Client

	// OnRefresh is called with the new tokens after each refresh.
	// Save the refresh token there; the old one stops working.
	OnRefresh func(Token) error

	mu    sync.Mutex
	token Token
}

// NewClient returns a client for the given country (DE, AT, ...) and
// language (de, en, ...), logged in with a refresh token.
func NewClient(country, language, refreshToken string) *Client {
	return &Client{
		Country:    strings.ToUpper(country),
		Language:   strings.ToLower(language),
		AppVersion: DefaultAppVersion,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		token:      Token{RefreshToken: refreshToken},
	}
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token.AccessToken != "" && time.Until(c.token.ExpiresAt) > time.Minute {
		return c.token.AccessToken, nil
	}
	if c.token.RefreshToken == "" {
		return "", ErrNotLoggedIn
	}

	tok, err := RefreshToken(ctx, c.HTTP, c.token.RefreshToken)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Status == 400 || apiErr.Status == 401) {
			return "", fmt.Errorf("%w: %v", ErrNotLoggedIn, err)
		}
		return "", err
	}
	c.token = tok
	if c.OnRefresh != nil {
		if err := c.OnRefresh(tok); err != nil {
			return "", err
		}
	}
	return tok.AccessToken, nil
}

func (c *Client) get(ctx context.Context, url string, out any) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("App-Version", c.AppVersion)
	req.Header.Set("Operating-System", "iOs")
	req.Header.Set("App", "com.lidl.eci.lidl.plus")
	req.Header.Set("Accept-Language", c.Language)
	return doJSON(c.HTTP, req, out)
}

func doJSON(hc *http.Client, req *http.Request, out any) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &APIError{Status: res.StatusCode, URL: req.URL.String(), Body: string(body)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("lidl: bad JSON from %s: %w", req.URL, err)
	}
	return nil
}
