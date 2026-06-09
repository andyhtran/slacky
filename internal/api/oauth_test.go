package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshUserTokenPostsRefreshGrantWithoutClientSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if request.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("grant_type = %q", request.Form.Get("grant_type"))
		}
		if request.Form.Get("client_id") != "client-id" {
			t.Fatalf("client_id = %q", request.Form.Get("client_id"))
		}
		if request.Form.Get("refresh_token") != "xoxr-old" {
			t.Fatalf("refresh_token = %q", request.Form.Get("refresh_token"))
		}
		if _, ok := request.Form["client_secret"]; ok {
			t.Fatalf("client_secret should be omitted for public PKCE refresh")
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok":            true,
			"access_token":  "xoxp-new",
			"token_type":    "user",
			"refresh_token": "xoxr-new",
			"expires_in":    43200,
			"scope":         "search:read,channels:read",
			"team":          map[string]string{"id": "T123", "name": "Sample Workspace"},
			"authed_user":   map[string]string{"id": "U123"},
		})
	}))
	defer server.Close()

	oldURL := OAuthAccessURL
	OAuthAccessURL = server.URL
	t.Cleanup(func() {
		OAuthAccessURL = oldURL
	})

	token, err := RefreshUserToken(context.Background(), server.Client(), OAuthRefreshRequest{
		ClientID:     "client-id",
		RefreshToken: "xoxr-old",
	}, "slacky-test")
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if token.AccessToken != "xoxp-new" || token.RefreshToken != "xoxr-new" {
		t.Fatalf("unexpected token response: %#v", token)
	}
	if token.ExpiresAt.IsZero() {
		t.Fatalf("expires_at should be populated from expires_in")
	}
	if len(token.Scopes) != 2 || token.Scopes[0] != "search:read" {
		t.Fatalf("unexpected scopes: %#v", token.Scopes)
	}
}

func TestRefreshUserTokenIncludesClientSecretWhenPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if request.Form.Get("client_secret") != "client-secret" {
			t.Fatalf("client_secret = %q", request.Form.Get("client_secret"))
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok":            true,
			"access_token":  "xoxp-new",
			"refresh_token": "xoxr-new",
			"expires_in":    43200,
		})
	}))
	defer server.Close()

	oldURL := OAuthAccessURL
	OAuthAccessURL = server.URL
	t.Cleanup(func() {
		OAuthAccessURL = oldURL
	})

	if _, err := RefreshUserToken(context.Background(), server.Client(), OAuthRefreshRequest{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RefreshToken: "xoxr-old",
	}, "slacky-test"); err != nil {
		t.Fatalf("refresh token: %v", err)
	}
}

func TestRefreshUserTokenReturnsOAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "error": "invalid_refresh_token"})
	}))
	defer server.Close()

	oldURL := OAuthAccessURL
	OAuthAccessURL = server.URL
	t.Cleanup(func() {
		OAuthAccessURL = oldURL
	})

	_, err := RefreshUserToken(context.Background(), server.Client(), OAuthRefreshRequest{
		ClientID:     "client-id",
		RefreshToken: "xoxr-old",
	}, "slacky-test")
	var oauthErr OAuthError
	if !errors.As(err, &oauthErr) {
		t.Fatalf("expected OAuthError, got %T %v", err, err)
	}
	if oauthErr.SlackError != "invalid_refresh_token" {
		t.Fatalf("SlackError = %q", oauthErr.SlackError)
	}
}
