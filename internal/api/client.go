package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const BaseURL = "https://slack.com/api/"

type Client struct {
	Token            string
	UserAgent        string
	HTTPClient       *http.Client
	MaxRateLimitWait time.Duration
	BaseURL          string
	Cooldown         func(method string) (RateLimitError, bool)
}

type SlackError struct {
	Method string
	Code   string
}

func (err SlackError) Error() string {
	return fmt.Sprintf("%s failed: %s", err.Method, err.Code)
}

type RateLimitError struct {
	Method     string        `json:"method"`
	RetryAfter time.Duration `json:"retry_after"`
	RetryAt    time.Time     `json:"retry_at"`
}

type CallMetadata struct {
	OAuthScopes []string `json:"oauth_scopes,omitempty"`
}

func (err RateLimitError) Error() string {
	return fmt.Sprintf("%s rate limited; retry after %s", err.Method, err.RetryAfter)
}

type AuthTestResult struct {
	URL    string   `json:"url,omitempty"`
	Team   string   `json:"team,omitempty"`
	User   string   `json:"user,omitempty"`
	TeamID string   `json:"team_id,omitempty"`
	UserID string   `json:"user_id,omitempty"`
	BotID  string   `json:"bot_id,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

func NewClient(token string, version string, timeout time.Duration, maxRateLimitWait time.Duration) *Client {
	return &Client{
		Token:            token,
		UserAgent:        "slacky/" + version,
		HTTPClient:       &http.Client{Timeout: timeout},
		MaxRateLimitWait: maxRateLimitWait,
		BaseURL:          BaseURL,
	}
}

func (client *Client) AuthTest(ctx context.Context) (AuthTestResult, error) {
	var response struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		URL    string `json:"url"`
		Team   string `json:"team"`
		User   string `json:"user"`
		TeamID string `json:"team_id"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	metadata, err := client.CallWithMetadata(ctx, "auth.test", url.Values{}, &response)
	if err != nil {
		return AuthTestResult{}, err
	}
	return AuthTestResult{
		URL:    response.URL,
		Team:   response.Team,
		User:   response.User,
		TeamID: response.TeamID,
		UserID: response.UserID,
		BotID:  response.BotID,
		Scopes: metadata.OAuthScopes,
	}, nil
}

func (client *Client) Call(ctx context.Context, method string, values url.Values, out any) error {
	_, err := client.CallWithMetadata(ctx, method, values, out)
	return err
}

func (client *Client) CallWithMetadata(ctx context.Context, method string, values url.Values, out any) (CallMetadata, error) {
	return client.call(ctx, method, values, out, true)
}

func (client *Client) call(ctx context.Context, method string, values url.Values, out any, allowRetry bool) (CallMetadata, error) {
	if client.Cooldown != nil {
		if rateLimit, ok := client.Cooldown(method); ok {
			return CallMetadata{}, rateLimit
		}
	}
	endpoint, err := url.Parse(client.BaseURL + method)
	if err != nil {
		return CallMetadata{}, err
	}
	endpoint.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return CallMetadata{}, err
	}
	request.Header.Set("Authorization", "Bearer "+client.Token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.UserAgent)

	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return CallMetadata{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusTooManyRequests {
		rateLimit := newRateLimitError(method, response.Header.Get("Retry-After"))
		if allowRetry && rateLimit.RetryAfter <= client.MaxRateLimitWait {
			timer := time.NewTimer(rateLimit.RetryAfter)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return CallMetadata{}, ctx.Err()
			case <-timer.C:
				return client.call(ctx, method, values, out, false)
			}
		}
		return CallMetadata{}, rateLimit
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return CallMetadata{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CallMetadata{}, fmt.Errorf("%s failed with HTTP %d", method, response.StatusCode)
	}
	var base struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &base); err != nil {
		return CallMetadata{}, err
	}
	if !base.OK {
		if base.Error == "" {
			base.Error = "unknown_error"
		}
		return CallMetadata{}, SlackError{Method: method, Code: base.Error}
	}
	metadata := CallMetadata{
		OAuthScopes: splitSlackScopes(response.Header.Get("X-OAuth-Scopes")),
	}
	return metadata, json.Unmarshal(body, out)
}

func newRateLimitError(method string, retryAfterHeader string) RateLimitError {
	seconds, err := strconv.Atoi(retryAfterHeader)
	if err != nil || seconds < 0 {
		seconds = 0
	}
	retryAfter := time.Duration(seconds) * time.Second
	return RateLimitError{
		Method:     method,
		RetryAfter: retryAfter,
		RetryAt:    time.Now().Add(retryAfter).UTC(),
	}
}
