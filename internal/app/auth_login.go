package app

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
)

const (
	defaultRedirectURI      = "http://127.0.0.1:8888/callback"
	defaultAuthPort         = 8888
	defaultAuthFallbackPort = 8889
	defaultAuthWaitTimeout  = 5 * time.Minute
)

type oauthCallback struct {
	Code  string
	State string
	Error string
}

func (cmd *AuthLoginCmd) Run(globals *Globals) error {
	normalized := cmd.withRuntimeDefaults()
	cmd = &normalized

	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	existing, _ := config.LoadAuth(pathSet.AuthFile.Path)
	clientID := firstNonEmpty(cmd.ClientID, existing.ClientID)
	clientSecret := firstNonEmpty(cmd.ClientSecret, existing.ClientSecret)
	redirectURI := firstNonEmpty(cmd.RedirectURI, existing.RedirectURI, defaultRedirectURI)
	if clientID == "" {
		return missingUsage("missing Slack client ID", "slacky auth login --client-id <id>", "slacky setup wizard", "slacky auth login --client-id 123.456 --manual")
	}

	pkce, err := api.NewPKCE()
	if err != nil {
		return err
	}
	state, err := api.NewState()
	if err != nil {
		return err
	}

	callbackURL := strings.TrimSpace(cmd.CallbackURL)
	if callbackURL == "" && (cmd.Manual || cmd.PasteCallback) {
		return cmd.runManual(globals, pathSet, existing, clientID, clientSecret, redirectURI, pkce, state)
	}
	if callbackURL != "" {
		callback, err := parseCallbackURL(callbackURL)
		if err != nil {
			return appError("invalid_callback_url", err.Error())
		}
		return cmd.exchangeAndStore(globals, pathSet, existing, clientID, clientSecret, redirectURI, pkce.Verifier, state, callback)
	}
	return cmd.runLocalCallback(globals, pathSet, existing, clientID, clientSecret, redirectURI, pkce, state)
}

func (cmd AuthLoginCmd) withRuntimeDefaults() AuthLoginCmd {
	if cmd.Port == 0 {
		cmd.Port = defaultAuthPort
	}
	if cmd.FallbackPort == 0 {
		cmd.FallbackPort = defaultAuthFallbackPort
	}
	if cmd.WaitTimeout == 0 {
		cmd.WaitTimeout = defaultAuthWaitTimeout
	}
	return cmd
}

func (cmd *AuthLoginCmd) runManual(globals *Globals, pathSet paths.Set, existing config.Auth, clientID string, clientSecret string, redirectURI string, pkce api.PKCEPair, state string) error {
	authURL := api.BuildUserAuthorizeURL(clientID, redirectURI, state, pkce.Challenge, api.UserScopes)
	fmt.Fprintln(os.Stderr, "Open this Slack authorization URL:")
	fmt.Fprintln(os.Stderr, authURL)
	if !output.IsTTY(os.Stdin) {
		err := appError("manual_callback_required", "manual auth requires --callback-url or interactive stdin")
		err.Examples = []string{"slacky auth login --manual --callback-url <final-localhost-url>"}
		return err
	}
	fmt.Fprint(os.Stderr, "Paste the final localhost URL after approval: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return appError("manual_callback_missing", "no callback URL was provided")
	}
	callback, err := parseCallbackURL(scanner.Text())
	if err != nil {
		return appError("invalid_callback_url", err.Error())
	}
	return cmd.exchangeAndStore(globals, pathSet, existing, clientID, clientSecret, redirectURI, pkce.Verifier, state, callback)
}

func (cmd *AuthLoginCmd) runLocalCallback(globals *Globals, pathSet paths.Set, existing config.Auth, clientID string, clientSecret string, redirectURI string, pkce api.PKCEPair, state string) error {
	actualRedirectURI, callbacks, shutdown, err := startOAuthListener(redirectURI, cmd.Port, cmd.FallbackPort)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	authURL := api.BuildUserAuthorizeURL(clientID, actualRedirectURI, state, pkce.Challenge, api.UserScopes)
	if cmd.NoOpen {
		fmt.Fprintln(os.Stderr, "Open this Slack authorization URL:")
		fmt.Fprintln(os.Stderr, authURL)
	} else if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
		fmt.Fprintln(os.Stderr, "Open this Slack authorization URL:")
		fmt.Fprintln(os.Stderr, authURL)
	}
	fmt.Fprintf(os.Stderr, "Waiting for Slack OAuth callback on %s\n", actualRedirectURI)

	select {
	case callback := <-callbacks:
		return cmd.exchangeAndStore(globals, pathSet, existing, clientID, clientSecret, actualRedirectURI, pkce.Verifier, state, callback)
	case <-time.After(cmd.WaitTimeout):
		return appError("oauth_timeout", "timed out waiting for Slack OAuth callback")
	}
}

func (cmd *AuthLoginCmd) exchangeAndStore(globals *Globals, pathSet paths.Set, existing config.Auth, clientID string, clientSecret string, redirectURI string, verifier string, expectedState string, callback oauthCallback) error {
	if callback.Error != "" {
		return appError("oauth_callback_error", callback.Error)
	}
	if callback.State != expectedState {
		return appError("oauth_state_mismatch", "Slack OAuth callback state did not match the login request")
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	token, err := api.ExchangeUserToken(ctx, &http.Client{Timeout: globals.Timeout}, api.OAuthExchangeRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Code:         callback.Code,
		CodeVerifier: verifier,
		RedirectURI:  redirectURI,
	}, "slacky/"+appVersion)
	if err != nil {
		return appError("oauth_exchange_failed", err.Error())
	}

	auth := existing
	auth.ClientID = clientID
	auth.ClientSecret = clientSecret
	auth.RedirectURI = redirectURI
	auth.UserToken = token.AccessToken
	auth.TokenType = token.TokenType
	auth.UserID = token.UserID
	auth.TeamID = token.TeamID
	auth.TeamName = token.TeamName
	auth.Scopes = token.Scopes
	auth.RefreshToken = token.RefreshToken
	auth.ExpiresAt = token.ExpiresAt
	identity, err := authTestImportedToken(globals, auth.UserToken)
	if err != nil {
		return err
	}
	auth.TeamID = firstNonEmpty(identity.TeamID, auth.TeamID)
	auth.TeamName = firstNonEmpty(identity.Team, auth.TeamName)
	auth.UserID = firstNonEmpty(identity.UserID, auth.UserID)
	auth.UserName = firstNonEmpty(identity.User, auth.UserName)
	if len(identity.Scopes) > 0 {
		auth.Scopes = append([]string{}, identity.Scopes...)
	}
	if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
		return err
	}

	summary := map[string]any{
		"path":              pathSet.AuthFile.Path,
		"team_id":           auth.TeamID,
		"team_name":         auth.TeamName,
		"user_id":           auth.UserID,
		"user_name":         auth.UserName,
		"token_type":        auth.TokenType,
		"scopes":            auth.Scopes,
		"has_user_token":    auth.UserToken != "",
		"has_refresh_token": auth.RefreshToken != "",
		"expires_at":        auth.ExpiresAt,
	}
	text := strings.Join([]string{
		"Auth login complete",
		"",
		fmt.Sprintf("Stored: %s", pathSet.AuthFile.Path),
		fmt.Sprintf("Team: %s", blank(auth.TeamName)),
		fmt.Sprintf("User: %s", authDisplayLabel(auth.UserID, auth.UserName)),
		fmt.Sprintf("Scopes: %d", len(auth.Scopes)),
		"",
		"Next:",
		"  slacky auth status",
		"  " + defaultSearchCommand(authMentionLabel(auth.UserID, auth.UserName)),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: summary,
	})
}

func startOAuthListener(redirectURI string, port int, fallbackPort int) (string, <-chan oauthCallback, func(context.Context) error, error) {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return "", nil, nil, err
	}
	if parsed.Scheme != "http" {
		return "", nil, nil, appError("invalid_redirect_uri", "auth login local callback requires an http://127.0.0.1 redirect URI")
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return "", nil, nil, appError("invalid_redirect_uri", "auth login local callback requires host 127.0.0.1 or localhost")
	}
	path := parsed.Path
	if path == "" {
		path = "/callback"
	}
	ports := candidatePorts(parsed.Port(), port, fallbackPort)
	var listener net.Listener
	var listenPort int
	for _, candidate := range ports {
		listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(candidate)))
		if err == nil {
			listenPort = candidate
			break
		}
	}
	if listener == nil {
		return "", nil, nil, err
	}

	callbacks := make(chan oauthCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		callback := oauthCallback{
			Code:  query.Get("code"),
			State: query.Get("state"),
			Error: query.Get("error"),
		}
		if callback.Error != "" {
			http.Error(writer, "Slack authorization failed. You can close this window.", http.StatusBadRequest)
		} else {
			_, _ = fmt.Fprintln(writer, "Slack authorization received. You can close this window.")
		}
		callbacks <- callback
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	actual := (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)),
		Path:   path,
	}).String()
	return actual, callbacks, server.Shutdown, nil
}

func candidatePorts(parsedPort string, port int, fallbackPort int) []int {
	ports := []int{}
	if parsedPort != "" {
		if parsed, err := strconv.Atoi(parsedPort); err == nil {
			ports = append(ports, parsed)
		}
	}
	if port != 0 && !containsInt(ports, port) {
		ports = append(ports, port)
	}
	if fallbackPort != 0 && !containsInt(ports, fallbackPort) {
		ports = append(ports, fallbackPort)
	}
	return ports
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func parseCallbackURL(raw string) (oauthCallback, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return oauthCallback{}, err
	}
	query := parsed.Query()
	callback := oauthCallback{
		Code:  query.Get("code"),
		State: query.Get("state"),
		Error: query.Get("error"),
	}
	if callback.Error != "" {
		return callback, nil
	}
	if callback.Code == "" {
		return oauthCallback{}, fmt.Errorf("callback URL is missing code")
	}
	if callback.State == "" {
		return oauthCallback{}, fmt.Errorf("callback URL is missing state")
	}
	return callback, nil
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "linux":
		return exec.Command("xdg-open", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
