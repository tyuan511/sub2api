package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type upstreamTestEncryptor struct{}

func (upstreamTestEncryptor) Encrypt(v string) (string, error) { return "encrypted:" + v, nil }
func (upstreamTestEncryptor) Decrypt(v string) (string, error) {
	return strings.TrimPrefix(v, "encrypted:"), nil
}

type upstreamTestRepo struct {
	u         Upstream
	recorded  *float64
	lastError string
	claimed   bool
}

func (r *upstreamTestRepo) List(context.Context) ([]Upstream, error) { return []Upstream{r.u}, nil }
func (r *upstreamTestRepo) Get(context.Context, int64) (*Upstream, error) {
	copy := r.u
	return &copy, nil
}
func (r *upstreamTestRepo) Save(_ context.Context, u *Upstream) (*Upstream, error) {
	r.u = *u
	return u, nil
}
func (r *upstreamTestRepo) Delete(context.Context, int64) error { return nil }
func (r *upstreamTestRepo) Claim(context.Context, int64, bool) (*Upstream, error) {
	if r.claimed {
		return nil, nil
	}
	r.claimed = true
	return &r.u, nil
}
func (r *upstreamTestRepo) RecordBalance(_ context.Context, _ *Upstream, balance *float64, lastError string, _ bool) error {
	r.recorded = balance
	r.lastError = lastError
	if balance != nil {
		r.u.Balance = balance
	}
	r.u.LastError = lastError
	return nil
}

func upstreamTestService(t *testing.T, handler http.HandlerFunc, kind string) (*UpstreamService, *upstreamTestRepo) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	repo := &upstreamTestRepo{u: Upstream{ID: 1, Name: "fixture", Type: kind, BaseURL: server.URL + "/mount/v1", Enabled: true,
		BalanceAPIKeyEncrypted: "encrypted:wallet-key", AccessTokenEncrypted: "encrypted:panel-token", UserID: 12,
		BalanceMultiplier: 1, QuotaPerUnit: 500000, BalanceThreshold: 10, CheckIntervalSeconds: 300, Revision: 1}}
	svc := NewUpstreamService(repo, upstreamTestEncryptor{}, nil, cfg)
	t.Cleanup(svc.Stop)
	return svc, repo
}

func TestUpstreamBalanceAdapters(t *testing.T) {
	for _, test := range []struct {
		name, kind, body, path string
		balance                float64
		fail                   bool
	}{
		{"wallet", "sub2api", `{"balance":19017.32971662,"unit":"USD","remaining":19017.32971662}`, "/mount/v1/usage", 19017.32971662, false},
		{"zero wallet", "sub2api", `{"balance":0}`, "/mount/v1/usage", 0, false},
		{"negative wallet", "sub2api", `{"balance":-2}`, "/mount/v1/usage", -2, false},
		{"quota is not wallet", "sub2api", `{"mode":"quota_limited","remaining":40}`, "/mount/v1/usage", 0, true},
		{"subscription is not wallet", "sub2api", `{"mode":"unrestricted","planName":"subscription","remaining":40}`, "/mount/v1/usage", 0, true},
		{"newapi quota", "newapi", `{"success":true,"data":{"quota":2500000}}`, "/mount/api/user/self", 5, false},
		{"newapi failure", "newapi", `{"success":false,"data":{"quota":2500000}}`, "/mount/api/user/self", 0, true},
		{"newapi missing quota", "newapi", `{"success":true,"data":{}}`, "/mount/api/user/self", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo := upstreamTestService(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, test.path, r.URL.Path)
				if test.kind == "sub2api" {
					require.Equal(t, "Bearer wallet-key", r.Header.Get("Authorization"))
				} else {
					require.Equal(t, "Bearer panel-token", r.Header.Get("Authorization"))
					require.Equal(t, "12", r.Header.Get("New-Api-User"))
				}
				fmt.Fprint(w, test.body)
			}, test.kind)
			balance, err := svc.queryBalance(context.Background(), &repo.u)
			if test.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.InDelta(t, test.balance, balance, 1e-8)
			}
		})
	}
}

func TestNewAPIManagementHostAndCompatibleTokens(t *testing.T) {
	for _, input := range []string{"legacy+fixture", "Bearer legacy+fixture", "nap_scoped_fixture", "bearer nap_scoped_fixture"} {
		t.Run(input, func(t *testing.T) {
			expected := strings.TrimPrefix(strings.TrimPrefix(input, "Bearer "), "bearer ")
			console := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/console/api/user/self", r.URL.Path)
				require.Equal(t, "Bearer "+expected, r.Header.Get("Authorization"))
				require.Equal(t, "12", r.Header.Get("New-Api-User"))
				fmt.Fprint(w, `{"success":true,"data":{"quota":3588193}}`)
			}))
			defer console.Close()
			svc, repo := upstreamTestService(t, func(http.ResponseWriter, *http.Request) {
				t.Error("A management credential must never be sent to the relay host")
			}, "newapi")
			repo.u.WebsiteURL = console.URL + "/console"
			repo.u.AccessTokenEncrypted = "encrypted:" + input
			balance, err := svc.queryBalance(context.Background(), &repo.u)
			require.NoError(t, err)
			require.InDelta(t, 7.176386, balance, 1e-8)
			credentials := map[string]any{"api_key": "manual-relay-key"}
			require.NoError(t, svc.BindAccount(context.Background(), 1, credentials, map[string]any{}))
			require.Equal(t, repo.u.BaseURL, credentials["base_url"])
			require.Equal(t, "manual-relay-key", credentials["api_key"])
		})
	}
}

func TestNewAPIManagementFailureDoesNotFallBackToRelay(t *testing.T) {
	console := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"nap_fixture_secret"}`)
	}))
	defer console.Close()
	svc, repo := upstreamTestService(t, func(http.ResponseWriter, *http.Request) {
		t.Error("Console failure must not leak the token to another host")
	}, "newapi")
	repo.u.WebsiteURL = console.URL
	_, err := svc.queryBalance(context.Background(), &repo.u)
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 403")
	require.NotContains(t, err.Error(), "nap_fixture_secret")
}

func TestNewAPIAccessTokenSaveNormalizesBearerAndPreservesSavedSecrets(t *testing.T) {
	svc, repo := upstreamTestService(t, func(http.ResponseWriter, *http.Request) {}, "newapi")
	token := " Bearer nap_fixture_token "
	input := UpstreamInput{Name: "console", Type: "newapi", BaseURL: repo.u.BaseURL,
		WebsiteURL: "https://console.example.com", UserID: 12, AccessToken: &token}
	u, err := svc.Save(context.Background(), 1, input)
	require.NoError(t, err)
	require.Equal(t, "encrypted:nap_fixture_token", u.AccessTokenEncrypted)
	input.AccessToken = nil
	u, err = svc.Save(context.Background(), 1, input)
	require.NoError(t, err)
	require.Equal(t, "encrypted:nap_fixture_token", u.AccessTokenEncrypted)
	for _, invalid := range []string{"Bearer", "Bearer token\r\nX-Injected: value", "token with spaces"} {
		input.AccessToken = &invalid
		_, err := svc.Save(context.Background(), 1, input)
		require.Error(t, err)
	}
}

func TestUpstreamRefreshKeepsLastBalanceAndHidesCredentials(t *testing.T) {
	svc, repo := upstreamTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"message":"wallet-key panel-token"}`)
	}, "sub2api")
	old := 23.0
	repo.u.Balance = &old
	u, err := svc.Refresh(context.Background(), 1, true)
	require.NoError(t, err)
	require.Equal(t, 23.0, *u.Balance)
	require.Nil(t, repo.recorded)
	require.Contains(t, u.LastError, "HTTP 401")
	require.NotContains(t, u.LastError, "wallet-key")
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "encrypted:")
	// A second refresh during the lease must not perform or record another query.
	_, err = svc.Refresh(context.Background(), 1, true)
	require.NoError(t, err)
}

func TestUpstreamRedirectDoesNotForwardCredential(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	svc, repo := upstreamTestService(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }, "sub2api")
	_, err := svc.queryBalance(context.Background(), &repo.u)
	require.Error(t, err)
	require.False(t, reached)
}

func TestUpstreamAccountUsesSelectedBaseURLAndManualKey(t *testing.T) {
	svc, repo := upstreamTestService(t, func(http.ResponseWriter, *http.Request) {
		t.Error("Binding a Base URL must not make upstream requests")
	}, "sub2api")
	credentials := map[string]any{"api_key": "manual"}
	extra := map[string]any{}
	err := svc.BindAccount(context.Background(), 1, credentials, extra)
	require.NoError(t, err)
	require.Equal(t, "manual", credentials["api_key"])
	require.Equal(t, repo.u.BaseURL, credentials["base_url"])
	require.Equal(t, int64(1), extra["upstream_id"])
	repo.u.Enabled = false
	require.Error(t, svc.BindAccount(context.Background(), 1, credentials, extra))
}

func TestUpstreamSavePreservesSecretsAndValidatesDefaults(t *testing.T) {
	svc, repo := upstreamTestService(t, func(http.ResponseWriter, *http.Request) {}, "sub2api")
	input := UpstreamInput{Name: "updated", Type: "sub2api", BaseURL: repo.u.BaseURL}
	_, err := svc.Save(context.Background(), 1, input)
	require.NoError(t, err)
	require.Equal(t, "encrypted:wallet-key", repo.u.BalanceAPIKeyEncrypted)
	token := "replacement"
	input.BalanceAPIKey = &token
	_, err = svc.Save(context.Background(), 1, input)
	require.NoError(t, err)
	require.Equal(t, "encrypted:replacement", repo.u.BalanceAPIKeyEncrypted)
	input.Type = "newapi"
	input.BalanceAPIKey = nil
	_, err = svc.Save(context.Background(), 1, input)
	require.Error(t, err) // Type changes cannot reuse a different provider's token.
	input.Type = "other"
	input.BaseURL = "https://user:password@example.com"
	_, err = svc.Save(context.Background(), 0, input)
	require.Error(t, err)
	input.BaseURL = "https://api.example.com"
	u, err := svc.Save(context.Background(), 0, input)
	require.NoError(t, err)
	require.Equal(t, 10.0, u.BalanceThreshold)
	require.Equal(t, 1.0, u.BalanceMultiplier)
	require.Equal(t, 300, u.CheckIntervalSeconds)
	require.Empty(t, u.AccessTokenEncrypted)
}

func TestUpstreamTelegramAlertFormatting(t *testing.T) {
	raw, _ := json.Marshal(UpstreamBalanceNotification{UpstreamID: 7, Name: "fixture", Balance: 8, Threshold: 10, WebsiteURL: "https://example.com"})
	text := formatTelegramOutboxText(UpstreamLowBalanceEvent, string(raw), supportTelegramNotificationPayload{})
	require.Contains(t, text, "上游余额不足")
	require.Contains(t, text, "fixture (#7)")
	require.Contains(t, text, "¥8.00")
	require.Contains(t, text, "¥10.00")
}
