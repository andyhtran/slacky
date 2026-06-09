package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	UserAuthorizeURL = "https://slack.com/oauth/v2_user/authorize"
	UserAccessURL    = "https://slack.com/api/oauth.v2.user.access"
)

type PKCEPair struct {
	Verifier  string
	Challenge string
}

type OAuthExchangeRequest struct {
	ClientID     string
	ClientSecret string
	Code         string
	CodeVerifier string
	RedirectURI  string
}

type OAuthToken struct {
	AccessToken  string
	TokenType    string
	UserID       string
	TeamID       string
	TeamName     string
	Scopes       []string
	RefreshToken string
	ExpiresAt    time.Time
}

type OAuthError struct {
	SlackError string
}

func (err OAuthError) Error() string {
	return "Slack OAuth failed: " + err.SlackError
}

func NewPKCE() (PKCEPair, error) {
	verifier, err := randomURLString(64)
	if err != nil {
		return PKCEPair{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	return PKCEPair{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

func NewState() (string, error) {
	return randomURLString(32)
}

func BuildUserAuthorizeURL(clientID, redirectURI, state, challenge string, scopes []string) string {
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("scope", strings.Join(scopes, ","))
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	return UserAuthorizeURL + "?" + values.Encode()
}

func ExchangeUserToken(ctx context.Context, client *http.Client, request OAuthExchangeRequest, userAgent string) (OAuthToken, error) {
	form := url.Values{}
	form.Set("client_id", request.ClientID)
	if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	form.Set("code", request.Code)
	form.Set("code_verifier", request.CodeVerifier)
	form.Set("redirect_uri", request.RedirectURI)
	form.Set("grant_type", "authorization_code")

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, UserAccessURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthToken{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", userAgent)

	response, err := client.Do(httpRequest)
	if err != nil {
		return OAuthToken{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	var payload oauthUserAccessResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return OAuthToken{}, err
	}
	if !payload.OK {
		if payload.Error == "" {
			payload.Error = fmt.Sprintf("http_status_%d", response.StatusCode)
		}
		return OAuthToken{}, OAuthError{SlackError: payload.Error}
	}
	return payload.token(), nil
}

type oauthUserAccessResponse struct {
	OK           bool              `json:"ok"`
	Error        string            `json:"error"`
	AccessToken  string            `json:"access_token"`
	TokenType    string            `json:"token_type"`
	Scope        string            `json:"scope"`
	RefreshToken string            `json:"refresh_token"`
	ExpiresIn    int               `json:"expires_in"`
	Team         oauthTeam         `json:"team"`
	AuthedUser   oauthAuthedUser   `json:"authed_user"`
	Enterprise   map[string]string `json:"enterprise"`
}

type oauthTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type oauthAuthedUser struct {
	ID           string `json:"id"`
	Scope        string `json:"scope"`
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (response oauthUserAccessResponse) token() OAuthToken {
	accessToken := response.AccessToken
	if accessToken == "" {
		accessToken = response.AuthedUser.AccessToken
	}
	tokenType := response.TokenType
	if tokenType == "" {
		tokenType = response.AuthedUser.TokenType
	}
	scope := response.Scope
	if scope == "" {
		scope = response.AuthedUser.Scope
	}
	refreshToken := response.RefreshToken
	if refreshToken == "" {
		refreshToken = response.AuthedUser.RefreshToken
	}
	expiresIn := response.ExpiresIn
	if expiresIn == 0 {
		expiresIn = response.AuthedUser.ExpiresIn
	}
	var expiresAt time.Time
	if expiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).UTC()
	}
	return OAuthToken{
		AccessToken:  accessToken,
		TokenType:    tokenType,
		UserID:       response.AuthedUser.ID,
		TeamID:       response.Team.ID,
		TeamName:     response.Team.Name,
		Scopes:       splitSlackScopes(scope),
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}
}

func splitSlackScopes(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' '
	})
	scopes := make([]string, 0, len(fields))
	for _, field := range fields {
		if field != "" {
			scopes = append(scopes, field)
		}
	}
	return scopes
}

func randomURLString(length int) (string, error) {
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
