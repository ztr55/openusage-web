package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/auth"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (p *Provider) resolveOAuthCredential(ctx context.Context, acct core.AccountConfig) (core.OAuthCredential, error) {
	if acct.OAuth == nil || strings.TrimSpace(acct.OAuth.AccessToken) == "" {
		return core.OAuthCredential{}, fmt.Errorf("%w: missing OAuth credential", errLiveUsageAuth)
	}
	credential := *acct.OAuth
	if strings.TrimSpace(credential.RefreshToken) != "" && credential.RefreshTokenExpiresAt > 0 && time.Now().UnixMilli() >= credential.RefreshTokenExpiresAt {
		return core.OAuthCredential{}, fmt.Errorf("%w: OAuth refresh token expired; sign in again", errLiveUsageAuth)
	}
	if !credential.IsExpired(time.Now().Add(5 * time.Minute)) {
		return credential, nil
	}
	return p.refreshOAuthCredential(ctx, acct, credential)
}

func (p *Provider) refreshOAuthCredential(ctx context.Context, acct core.AccountConfig, credential core.OAuthCredential) (core.OAuthCredential, error) {
	client := p.oauthClient
	if client == nil {
		client = auth.NewOAuthClient(p.Client())
	}
	var rejected error
	refresh := func(current core.OAuthCredential) (core.OAuthCredential, error) {
		if strings.TrimSpace(current.RefreshToken) == "" {
			return core.OAuthCredential{}, fmt.Errorf("OAuth token expired and cannot be refreshed")
		}
		if current.RefreshTokenExpiresAt > 0 && time.Now().UnixMilli() >= current.RefreshTokenExpiresAt {
			return core.OAuthCredential{}, fmt.Errorf("OAuth refresh token expired")
		}
		updated, err := client.RefreshCodexCredential(ctx, current)
		if errors.Is(err, auth.ErrRefreshTokenRejected) && strings.TrimSpace(acct.ID) != "" {
			current.RefreshTokenExpiresAt = time.Now().UnixMilli()
			rejected = err
			return current, nil
		}
		return updated, err
	}
	if strings.TrimSpace(acct.ID) == "" {
		refreshed, err := refresh(credential)
		if err != nil {
			return core.OAuthCredential{}, fmt.Errorf("%w: token refresh failed: %v", errLiveUsageAuth, err)
		}
		return refreshed, nil
	}
	updated, refreshed, err := config.RefreshOAuthCredential(acct.ID, credential, refresh)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("%w: token refresh failed: %v", errLiveUsageAuth, err)
	}
	if updated.AccessToken == "" {
		return core.OAuthCredential{}, fmt.Errorf("%w: OAuth credential was removed during refresh", errLiveUsageAuth)
	}
	if rejected != nil {
		return core.OAuthCredential{}, fmt.Errorf("%w: refresh token rejected; sign in again", errLiveUsageAuth)
	}
	if !refreshed {
		if updated.IsExpired(time.Now().Add(5 * time.Minute)) {
			return core.OAuthCredential{}, fmt.Errorf("%w: OAuth credential changed but is expired", errLiveUsageAuth)
		}
		return updated, nil
	}
	return updated, nil
}
