package lidl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	authAPI      = "https://accounts.lidl.com"
	clientID     = "LidlPlusNativeClient"
	clientSecret = "secret"
	redirectURI  = "com.lidlplus.app://callback"
	scopes       = "openid profile offline_access lpprofile lpapis"
)

// Token holds the tokens Lidl returns after a login or a refresh.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// NewVerifier returns a random PKCE verifier for one login attempt.
func NewVerifier() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// AuthorizeURL returns the Lidl login page URL for the given verifier.
func AuthorizeURL(verifier, country, language string) string {
	country = strings.ToUpper(country)
	language = strings.ToLower(language)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"scope":                 {scopes},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"Country":               {country},
		"language":              {language + "-" + country},
	}
	return authAPI + "/connect/authorize?" + q.Encode()
}

var (
	callbackCode = regexp.MustCompile(`[?&#]code=([^&\s#]+)`)
	bareCode     = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// ParseCallback returns the authorization code from a callback URL
// (com.lidlplus.app://callback?code=...) or from a bare code.
func ParseCallback(input string) (string, error) {
	input = strings.Trim(strings.TrimSpace(input), `"'`)
	if m := callbackCode.FindStringSubmatch(input); m != nil {
		return url.QueryUnescape(m[1])
	}
	if bareCode.MatchString(input) {
		return input, nil
	}
	return "", errors.New("lidl: no authorization code found in input")
}

// ExchangeCode trades the authorization code from the login callback for tokens.
func ExchangeCode(ctx context.Context, hc *http.Client, code, verifier string) (Token, error) {
	return requestToken(ctx, hc, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
}

// RefreshToken trades a refresh token for new tokens. Lidl rotates the
// refresh token on every call, so the returned one replaces the old one.
func RefreshToken(ctx context.Context, hc *http.Client, refreshToken string) (Token, error) {
	tok, err := requestToken(ctx, hc, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return Token{}, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

func requestToken(ctx context.Context, hc *http.Client, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authAPI+"/connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := doJSON(hc, req, &body); err != nil {
		return Token{}, err
	}
	if body.AccessToken == "" {
		return Token{}, errors.New("lidl: token response has no access token")
	}
	return Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}
