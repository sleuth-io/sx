package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
	"github.com/sleuth-io/sx/internal/buildinfo"
)

const (
	// DefaultPollInterval is the default interval for polling the token endpoint
	DefaultPollInterval = 5 * time.Second

	// OAuthClientID is the well-known client ID for the Skills CLI
	OAuthClientID = "sleuth-skills-claude-code"
)

// OAuthDeviceCodeResponse represents the response from the device authorization endpoint
type OAuthDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval,omitempty"`
}

// OAuthTokenResponse represents the response from the token endpoint
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// OAuthClient handles OAuth device code flow
type OAuthClient struct {
	serverURL    string
	httpClient   *http.Client
	pollInterval time.Duration
}

// NewOAuthClient creates a new OAuth client
func NewOAuthClient(serverURL string) *OAuthClient {
	return &OAuthClient{
		serverURL:    serverURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		pollInterval: DefaultPollInterval,
	}
}

// StartDeviceFlow initiates the OAuth device code flow
func (o *OAuthClient) StartDeviceFlow(ctx context.Context) (*OAuthDeviceCodeResponse, error) {
	endpoint := o.serverURL + "/api/oauth/device-authorization/"

	// Prepare request body with client_id
	data := url.Values{}
	data.Set("client_id", OAuthClientID)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device authorization failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var deviceResp OAuthDeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Set poll interval from response or use default
	if deviceResp.Interval > 0 {
		o.pollInterval = time.Duration(deviceResp.Interval) * time.Second
	}

	return &deviceResp, nil
}

// PollForToken polls the token endpoint until the user completes authorization
func (o *OAuthClient) PollForToken(ctx context.Context, deviceCode string) (*OAuthTokenResponse, error) {
	endpoint := o.serverURL + "/api/oauth/token/"

	ticker := time.NewTicker(o.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			token, err := o.requestToken(ctx, endpoint, deviceCode)
			if err != nil {
				return nil, err
			}

			// Check for specific error codes
			switch token.Error {
			case "":
				// Success - we have a token
				return token, nil
			case "authorization_pending":
				// User hasn't authorized yet, continue polling
				continue
			case "slow_down":
				// Slow down polling
				o.pollInterval += 5 * time.Second
				ticker.Reset(o.pollInterval)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired")
			case "access_denied":
				return nil, fmt.Errorf("authorization denied by user")
			default:
				return nil, fmt.Errorf("authorization failed: %s (%s)", token.ErrorDesc, token.Error)
			}
		}
	}
}

// requestToken makes a single token request
func (o *OAuthClient) requestToken(ctx context.Context, endpoint, deviceCode string) (*OAuthTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)
	data.Set("client_id", OAuthClientID)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &tokenResp, nil
}

// OpenBrowser opens the verification URI in the user's default browser
func OpenBrowser(verificationURI string) error {
	return browser.OpenURL(verificationURI)
}
