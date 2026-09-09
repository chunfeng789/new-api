package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("github", &GitHubProvider{})
}

// GitHubProvider implements OAuth for GitHub
type GitHubProvider struct{}

type gitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type gitHubUser struct {
	Id    int64  `json:"id"`    // GitHub numeric ID (permanent, never changes)
	Login string `json:"login"` // GitHub username (can be changed by user)
	Name  string `json:"name"`
	Email string `json:"email"`
}

// gitHubUserEmail is one entry of the authenticated user's email list.
type gitHubUserEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func (p *GitHubProvider) GetName() string {
	return "GitHub"
}

func (p *GitHubProvider) IsEnabled() bool {
	return common.GitHubOAuthEnabled
}

func (p *GitHubProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	values := map[string]string{
		"client_id":     common.GitHubClientId,
		"client_secret": common.GitHubClientSecret,
		"code":          code,
	}
	jsonData, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "GitHub"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-GitHub] ExchangeToken response status: %d", res.StatusCode)

	var oAuthResponse gitHubOAuthResponse
	err = json.NewDecoder(res.Body).Decode(&oAuthResponse)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if oAuthResponse.AccessToken == "" {
		logger.LogError(ctx, "[OAuth-GitHub] ExchangeToken failed: empty access token")
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "GitHub"})
	}

	logger.LogDebug(ctx, "[OAuth-GitHub] ExchangeToken success: scope=%s", oAuthResponse.Scope)

	return &OAuthToken{
		AccessToken: oAuthResponse.AccessToken,
		TokenType:   oAuthResponse.TokenType,
		Scope:       oAuthResponse.Scope,
	}, nil
}

func (p *GitHubProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo: fetching user info")

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))

	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "GitHub"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo response status: %d", res.StatusCode)

	// Check for non-200 status codes before attempting to decode
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500] + "..."
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo failed: status=%d, body=%s", res.StatusCode, bodyStr))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "GitHub"}, fmt.Sprintf("status %d", res.StatusCode))
	}

	var githubUser gitHubUser
	err = json.NewDecoder(res.Body).Decode(&githubUser)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-GitHub] GetUserInfo decode error: %s", err.Error()))
		return nil, err
	}

	if githubUser.Id == 0 || githubUser.Login == "" {
		logger.LogError(ctx, "[OAuth-GitHub] GetUserInfo failed: empty id or login field")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "GitHub"})
	}

	// /user only carries the optional public profile address, so it is empty for
	// every account that keeps its email private. The account's real address is
	// exposed by /user/emails, which the granted user:email scope unlocks.
	email := strings.TrimSpace(githubUser.Email)
	if email == "" || isGitHubNoReplyEmail(email) {
		email = p.fetchVerifiedEmail(ctx, token)
	}

	logger.LogDebug(ctx, "[OAuth-GitHub] GetUserInfo success: id=%d, login=%s, name=%s, email=%s",
		githubUser.Id, githubUser.Login, githubUser.Name, email)

	return &OAuthUser{
		ProviderUserID: strconv.FormatInt(githubUser.Id, 10), // Use numeric ID as primary identifier
		Username:       githubUser.Login,
		DisplayName:    githubUser.Name,
		Email:          email,
		Extra: map[string]any{
			"legacy_id": githubUser.Login, // Store login for migration from old accounts
		},
	}, nil
}

// fetchVerifiedEmail reads the account's email list, the only place GitHub
// exposes a private address. Failing to read it must not break the login, so
// every failure degrades to an account without an email.
func (p *GitHubProvider) fetchVerifiedEmail(ctx context.Context, token *OAuthToken) string {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		logger.LogWarn(ctx, "[OAuth-GitHub] GetUserEmails request error: %s", err.Error())
		return ""
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.AccessToken))
	req.Header.Set("Accept", "application/vnd.github+json")

	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogWarn(ctx, "[OAuth-GitHub] GetUserEmails error: %s", err.Error())
		return ""
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// 403 here means the token was issued without the user:email scope.
		logger.LogWarn(ctx, "[OAuth-GitHub] GetUserEmails failed: status=%d", res.StatusCode)
		return ""
	}

	var emails []gitHubUserEmail
	if err := common.DecodeJson(res.Body, &emails); err != nil {
		logger.LogWarn(ctx, "[OAuth-GitHub] GetUserEmails decode error: %s", err.Error())
		return ""
	}

	email := selectGitHubEmail(emails)
	if email == "" {
		logger.LogWarn(ctx, "[OAuth-GitHub] GetUserEmails returned no usable address (%d entries)", len(emails))
	}
	return email
}

// selectGitHubEmail picks the address to store for a GitHub account: the
// primary one, falling back to any other. Unverified addresses are never
// trusted, because anyone can list an address they do not own, and noreply
// aliases are skipped because they cannot receive the notification and
// recovery mail an account address exists for.
func selectGitHubEmail(emails []gitHubUserEmail) string {
	fallback := ""
	for _, entry := range emails {
		email := strings.TrimSpace(entry.Email)
		if email == "" || !entry.Verified || isGitHubNoReplyEmail(email) {
			continue
		}
		if entry.Primary {
			return email
		}
		if fallback == "" {
			fallback = email
		}
	}
	return fallback
}

func isGitHubNoReplyEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@users.noreply.github.com")
}

func (p *GitHubProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsGitHubIdAlreadyTaken(providerUserID)
}

func (p *GitHubProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.GitHubId = providerUserID
	return user.FillUserByGitHubId()
}

func (p *GitHubProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}

func (p *GitHubProvider) GetProviderPrefix() string {
	return "github_"
}

// ProviderUserIDColumn returns the users-table column storing this provider's user ID.
func (p *GitHubProvider) ProviderUserIDColumn() string {
	return "github_id"
}
