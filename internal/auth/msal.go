// Package auth acquires Microsoft Graph tokens with MSAL's public client flows and
// keeps the token cache in the macOS Keychain.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"

	"github.com/sd-strax/daakiya/internal/secrets"
)

var Scopes = []string{
	"https://graph.microsoft.com/Mail.ReadWrite",
	"https://graph.microsoft.com/MailboxSettings.ReadWrite",
	"https://graph.microsoft.com/User.Read",
}

var ErrSignInRequired = errors.New("sign-in required: run `daakiya auth`")

type Authenticator struct {
	client public.Client
	cache  fileCache
}

// New builds a public client whose token cache is an encrypted file under
// dataDir, keyed from the Keychain.
func New(clientID, tenantID string, sec secrets.Store, dataDir string) (*Authenticator, error) {
	if tenantID == "" {
		tenantID = "common"
	}
	fc := fileCache{secrets: sec, path: filepath.Join(dataDir, CacheFileName)}
	c, err := public.New(clientID,
		public.WithAuthority("https://login.microsoftonline.com/"+tenantID),
		public.WithCache(fc),
	)
	if err != nil {
		return nil, err
	}
	return &Authenticator{client: c, cache: fc}, nil
}

// Token returns an access token from the cache, refreshing silently. It never
// prompts; callers that can prompt use SignIn.
func (a *Authenticator) Token(ctx context.Context) (string, error) {
	accounts, err := a.client.Accounts(ctx)
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 {
		return "", ErrSignInRequired
	}
	res, err := a.client.AcquireTokenSilent(ctx, Scopes, public.WithSilentAccount(accounts[0]))
	if err != nil {
		return "", fmt.Errorf("%w (%v)", ErrSignInRequired, err)
	}
	return res.AccessToken, nil
}

// SignIn runs the device code flow, writing the user instructions to w.
func (a *Authenticator) SignIn(ctx context.Context, w io.Writer) (username string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	dc, err := a.client.AcquireTokenByDeviceCode(ctx, Scopes)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(w, dc.Result.Message)
	res, err := dc.AuthenticationResult(ctx)
	if err != nil {
		return "", err
	}
	return res.Account.PreferredUsername, nil
}

// SignOut removes cached accounts and deletes the cache file and its key.
func (a *Authenticator) SignOut(ctx context.Context) error {
	if accounts, err := a.client.Accounts(ctx); err == nil {
		for _, acc := range accounts {
			_ = a.client.RemoveAccount(ctx, acc)
		}
	}
	return a.cache.Clear()
}
