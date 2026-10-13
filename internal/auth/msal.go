// Package auth acquires Microsoft Graph tokens with MSAL's public client flows and
// keeps the token cache in the macOS Keychain.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"

	"github.com/sd-strax/daakiya/internal/secrets"
)

var Scopes = []string{
	"https://graph.microsoft.com/Mail.ReadWrite",
	"https://graph.microsoft.com/MailboxSettings.ReadWrite",
	"https://graph.microsoft.com/User.Read",
}

var ErrSignInRequired = errors.New("sign-in required: run `daakiya auth`")

// keychainCache persists MSAL's opaque cache blob as one Keychain item.
type keychainCache struct{ secrets secrets.Store }

func (k keychainCache) Replace(ctx context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	b64, err := k.secrets.Get(secrets.AccountMSALCache)
	if errors.Is(err, secrets.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil // corrupt cache: start fresh rather than fail
	}
	return c.Unmarshal(data)
}

func (k keychainCache) Export(ctx context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	return k.secrets.Set(secrets.AccountMSALCache, base64.StdEncoding.EncodeToString(data))
}

type Authenticator struct {
	client public.Client
}

func New(clientID, tenantID string, sec secrets.Store) (*Authenticator, error) {
	if tenantID == "" {
		tenantID = "common"
	}
	c, err := public.New(clientID,
		public.WithAuthority("https://login.microsoftonline.com/"+tenantID),
		public.WithCache(keychainCache{secrets: sec}),
	)
	if err != nil {
		return nil, err
	}
	return &Authenticator{client: c}, nil
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

// SignOut removes cached accounts.
func (a *Authenticator) SignOut(ctx context.Context) error {
	accounts, err := a.client.Accounts(ctx)
	if err != nil {
		return err
	}
	for _, acc := range accounts {
		if err := a.client.RemoveAccount(ctx, acc); err != nil {
			return err
		}
	}
	return nil
}
