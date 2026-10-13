package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type UpstreamService struct {
	repo       UpstreamRepository
	encryptor  SecretEncryptor
	telegram   *SupportTelegramService
	cfg        *config.Config
	httpClient *http.Client
	cancel     context.CancelFunc
	done       chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
}

func NewUpstreamService(repo UpstreamRepository, encryptor SecretEncryptor, telegram *SupportTelegramService, cfg *config.Config) *UpstreamService {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg == nil || !cfg.Security.URLAllowlist.AllowPrivateHosts {
		transport.DialContext = safeDialContext
		transport.Proxy = nil // A proxy must not bypass destination address validation.
	}
	return &UpstreamService{repo: repo, encryptor: encryptor, telegram: telegram, cfg: cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second, Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, done: make(chan struct{})}
}

func ProvideUpstreamService(repo UpstreamRepository, encryptor SecretEncryptor, telegram *SupportTelegramService, cfg *config.Config) *UpstreamService {
	s := NewUpstreamService(repo, encryptor, telegram, cfg)
	s.Start()
	return s
}

func (s *UpstreamService) List(ctx context.Context) ([]Upstream, error) { return s.repo.List(ctx) }
func (s *UpstreamService) Delete(ctx context.Context, id int64) error   { return s.repo.Delete(ctx, id) }

func upstreamInvalid(message string) error {
	return infraerrors.BadRequest("UPSTREAM_INVALID", message)
}

func upstreamURL(raw string, required bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && !required {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", upstreamInvalid("Use an HTTP(S) URL without credentials, query parameters or fragments")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *UpstreamService) Save(ctx context.Context, id int64, input UpstreamInput) (*Upstream, error) {
	u := &Upstream{BalanceThreshold: 10, BalanceMultiplier: 1, QuotaPerUnit: 500000, CheckIntervalSeconds: 300, Enabled: true}
	if id > 0 {
		var err error
		u, err = s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	previousType := u.Type
	u.Name = strings.TrimSpace(input.Name)
	u.Type = input.Type
	if u.Name == "" || len([]rune(u.Name)) > 100 {
		return nil, upstreamInvalid("Name is required and must be at most 100 characters")
	}
	if u.Type != "sub2api" && u.Type != "newapi" && u.Type != "other" {
		return nil, upstreamInvalid("Unknown upstream type")
	}
	var err error
	if u.BaseURL, err = upstreamURL(input.BaseURL, true); err != nil {
		return nil, err
	}
	if _, err = cnValidateProbeURL(s.cfg, u.BaseURL); err != nil {
		return nil, upstreamInvalid("Base URL is rejected by the outbound URL policy")
	}
	if u.WebsiteURL, err = upstreamURL(input.WebsiteURL, false); err != nil {
		return nil, err
	}
	if u.Type == "newapi" && u.WebsiteURL != "" {
		if _, err := cnValidateProbeURL(s.cfg, u.WebsiteURL); err != nil {
			return nil, upstreamInvalid("Management URL is rejected by the outbound URL policy")
		}
	}
	if previousType != "" && previousType != u.Type {
		u.AccessTokenEncrypted = ""
		u.BalanceAPIKeyEncrypted = ""
	}
	u.UserID = input.UserID
	if input.BalanceThreshold != nil {
		u.BalanceThreshold = *input.BalanceThreshold
	}
	if input.BalanceMultiplier != nil {
		u.BalanceMultiplier = *input.BalanceMultiplier
	}
	if input.QuotaPerUnit != nil {
		u.QuotaPerUnit = *input.QuotaPerUnit
	}
	if input.CheckIntervalSeconds != nil {
		u.CheckIntervalSeconds = *input.CheckIntervalSeconds
	}
	if input.Enabled != nil {
		u.Enabled = *input.Enabled
	}
	for _, v := range []float64{u.BalanceThreshold, u.BalanceMultiplier, u.QuotaPerUnit} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, upstreamInvalid("Amounts must be finite")
		}
	}
	if u.BalanceThreshold < 0 || u.BalanceThreshold > 1e10 || u.BalanceMultiplier <= 0 || u.BalanceMultiplier > 1e8 || u.QuotaPerUnit <= 0 || u.QuotaPerUnit > 1e10 || u.CheckIntervalSeconds < 60 || u.CheckIntervalSeconds > 86400 {
		return nil, upstreamInvalid("Invalid threshold, conversion multiplier, quota unit or check interval")
	}
	for _, item := range []struct {
		plain     *string
		encrypted *string
	}{{input.BalanceAPIKey, &u.BalanceAPIKeyEncrypted}, {input.AccessToken, &u.AccessTokenEncrypted}} {
		if item.plain == nil {
			continue
		}
		plain := strings.TrimSpace(*item.plain)
		if u.Type == "newapi" && item.encrypted == &u.AccessTokenEncrypted && plain != "" {
			plain, err = normalizeNewAPIAccessToken(plain)
			if err != nil {
				return nil, err
			}
		}
		if strings.ContainsAny(plain, "\r\n") || len(plain) > 8192 {
			return nil, upstreamInvalid("Invalid credential")
		}
		if plain == "" {
			*item.encrypted = ""
			continue
		}
		encrypted, err := s.encryptor.Encrypt(plain)
		if err != nil {
			return nil, err
		}
		*item.encrypted = encrypted
	}
	if u.Type == "sub2api" && u.BalanceAPIKeyEncrypted == "" {
		return nil, upstreamInvalid("A balance query API key is required for sub2api")
	}
	if u.Type == "newapi" && (u.UserID <= 0 || u.AccessTokenEncrypted == "") {
		return nil, upstreamInvalid("A user ID and access token are required for newapi")
	}
	if u.Type == "other" {
		u.UserID = 0
		u.AccessTokenEncrypted = ""
		u.BalanceAPIKeyEncrypted = ""
	}
	if u.Type == "newapi" {
		u.BalanceAPIKeyEncrypted = ""
	}
	if u.Type == "sub2api" {
		u.AccessTokenEncrypted = ""
		u.UserID = 0
	}
	return s.repo.Save(ctx, u)
}

// Gateway Base URLs may end in /v1; management APIs are relative to the site mount point.
func upstreamEndpoint(base, path string) string {
	root := strings.TrimRight(base, "/")
	root = strings.TrimSuffix(root, "/v1")
	return root + path
}

// Both legacy system tokens and scoped PATs use the same authorization protocol.
// Accept a copied Authorization value without adding a second Bearer prefix.
func normalizeNewAPIAccessToken(raw string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1], nil
	}
	if len(parts) != 1 || strings.EqualFold(parts[0], "Bearer") {
		return "", upstreamInvalid("Enter a newapi system access token or scoped personal access token")
	}
	return parts[0], nil
}

func (s *UpstreamService) request(ctx context.Context, u *Upstream, path string, out any) error {
	baseURL := u.BaseURL
	// Newapi's console and relay can be separate hosts. Management credentials
	// must be sent to the console, while account creation keeps the relay URL.
	if u.Type == "newapi" && u.WebsiteURL != "" {
		baseURL = u.WebsiteURL
	}
	endpoint, err := cnValidateProbeURL(s.cfg, upstreamEndpoint(baseURL, path))
	if err != nil {
		return ErrUpstreamQuery.WithCause(errors.New("URL rejected by outbound policy"))
	}
	encrypted := u.BalanceAPIKeyEncrypted
	if u.Type == "newapi" {
		encrypted = u.AccessTokenEncrypted
	}
	if encrypted == "" {
		return upstreamInvalid("Upstream credential is required")
	}
	secret, err := s.encryptor.Decrypt(encrypted)
	if err != nil {
		return ErrUpstreamQuery.WithCause(errors.New("Cannot decrypt upstream credential"))
	}
	if u.Type == "newapi" {
		secret, err = normalizeNewAPIAccessToken(secret)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrUpstreamQuery
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sub2api-upstream-manager/1.0")
	if u.Type == "newapi" {
		req.Header.Set("New-Api-User", strconv.FormatInt(u.UserID, 10))
	}
	res, err := s.httpClient.Do(req)
	if err != nil {
		return ErrUpstreamQuery.WithCause(errors.New("Upstream connection failed"))
	}
	defer res.Body.Close()
	// Never expose upstream response bodies: an upstream may echo credentials.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return infraerrors.BadRequest("UPSTREAM_QUERY_FAILED", fmt.Sprintf("Upstream returned HTTP %d", res.StatusCode))
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 {
		return ErrUpstreamQuery.WithCause(errors.New("Upstream response too large or unreadable"))
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return ErrUpstreamQuery.WithCause(errors.New("Invalid upstream JSON response"))
	}
	return nil
}

func (s *UpstreamService) queryBalance(ctx context.Context, u *Upstream) (float64, error) {
	var value float64
	switch u.Type {
	case "sub2api":
		var response struct {
			Balance *float64 `json:"balance"`
			Mode    string   `json:"mode"`
			IsValid *bool    `json:"isValid"`
		}
		if err := s.request(ctx, u, "/v1/usage", &response); err != nil {
			return 0, err
		}
		// A key quota or subscription remaining amount is not the wallet balance.
		if response.Balance == nil || (response.IsValid != nil && !*response.IsValid) {
			return 0, infraerrors.BadRequest("UPSTREAM_BALANCE_UNAVAILABLE", "Use an unrestricted wallet API key; key quotas and subscriptions are not wallet balances")
		}
		value = *response.Balance
	case "newapi":
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Quota *float64 `json:"quota"`
			} `json:"data"`
		}
		if err := s.request(ctx, u, "/api/user/self", &response); err != nil {
			return 0, err
		}
		if !response.Success || response.Data.Quota == nil {
			return 0, ErrUpstreamQuery
		}
		value = *response.Data.Quota / u.QuotaPerUnit
	default:
		return 0, ErrUpstreamUnsupported
	}
	value *= u.BalanceMultiplier
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) >= 1e12 {
		return 0, ErrUpstreamQuery
	}
	return value, nil
}

func (s *UpstreamService) Refresh(ctx context.Context, id int64, force bool) (*Upstream, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Type == "other" {
		return nil, ErrUpstreamUnsupported
	}
	if !current.Enabled {
		return nil, upstreamInvalid("Upstream polling is disabled")
	}
	u, err := s.repo.Claim(ctx, id, force)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return s.repo.Get(ctx, id)
	}
	balance, queryErr := s.queryBalance(ctx, u)
	var balancePtr *float64
	lastError := ""
	notify := false
	if queryErr != nil {
		lastError = infraerrors.Message(queryErr)
	} else {
		balancePtr = &balance
		if s.telegram != nil {
			cfg, err := s.telegram.loadStored(ctx)
			notify = err == nil && cfg.Enabled && cfg.BotTokenEncrypted != ""
		}
	}
	if err := s.repo.RecordBalance(ctx, u, balancePtr, lastError, notify); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// BindAccount snapshots the upstream endpoint while keeping the operator's API key.
func (s *UpstreamService) BindAccount(ctx context.Context, id int64, credentials, extra map[string]any) error {
	u, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if !u.Enabled {
		return upstreamInvalid("Selected upstream is disabled")
	}
	if credentials == nil || extra == nil {
		return upstreamInvalid("Credentials and metadata are required")
	}
	credentials["base_url"] = u.BaseURL
	extra["upstream_id"] = u.ID
	return nil
}

func (s *UpstreamService) Start() {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		go func() {
			defer close(s.done)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				s.poll(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
func (s *UpstreamService) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
		s.httpClient.CloseIdleConnections()
	})
}
func (s *UpstreamService) poll(ctx context.Context) {
	list, err := s.repo.List(ctx)
	if err != nil {
		slog.Warn("upstream polling list failed")
		return
	}
	// A bounded worker pool prevents one slow upstream from delaying every other upstream.
	jobs := make(chan Upstream)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for u := range jobs {
				queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				_, err := s.Refresh(queryCtx, u.ID, false)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("upstream polling failed", "upstream_id", u.ID)
				}
			}
		}()
	}
	for _, u := range list {
		if !u.Enabled || u.Type == "other" {
			continue
		}
		select {
		case jobs <- u:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}
