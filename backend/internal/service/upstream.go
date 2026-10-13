package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUpstreamNotFound    = infraerrors.NotFound("UPSTREAM_NOT_FOUND", "Upstream not found")
	ErrUpstreamInvalid     = infraerrors.BadRequest("UPSTREAM_INVALID", "Invalid upstream configuration")
	ErrUpstreamUnsupported = infraerrors.BadRequest("UPSTREAM_UNSUPPORTED", "This upstream does not support automatic balance queries")
	ErrUpstreamQuery       = infraerrors.BadRequest("UPSTREAM_QUERY_FAILED", "Upstream query failed")
)

const UpstreamLowBalanceEvent = "upstream_low_balance"

type Upstream struct {
	ID                     int64      `json:"id"`
	Name                   string     `json:"name"`
	Type                   string     `json:"type"`
	WebsiteURL             string     `json:"website_url"`
	BaseURL                string     `json:"base_url"`
	UserID                 int64      `json:"user_id"`
	BalanceThreshold       float64    `json:"balance_threshold"`
	BalanceMultiplier      float64    `json:"balance_multiplier"`
	QuotaPerUnit           float64    `json:"quota_per_unit"`
	CheckIntervalSeconds   int        `json:"check_interval_seconds"`
	Enabled                bool       `json:"enabled"`
	Balance                *float64   `json:"balance"`
	BalanceCheckedAt       *time.Time `json:"balance_checked_at"`
	LastCheckAt            *time.Time `json:"last_check_at"`
	LastError              string     `json:"last_error"`
	HasBalanceAPIKey       bool       `json:"has_balance_api_key"`
	HasAccessToken         bool       `json:"has_access_token"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	BalanceAPIKeyEncrypted string     `json:"-"`
	AccessTokenEncrypted   string     `json:"-"`
	Revision               int64      `json:"-"`
}

// Pointer secrets distinguish preserving an existing secret from explicitly clearing it.
type UpstreamInput struct {
	Name                 string   `json:"name"`
	Type                 string   `json:"type"`
	WebsiteURL           string   `json:"website_url"`
	BaseURL              string   `json:"base_url"`
	BalanceAPIKey        *string  `json:"balance_api_key"`
	AccessToken          *string  `json:"access_token"`
	UserID               int64    `json:"user_id"`
	BalanceThreshold     *float64 `json:"balance_threshold"`
	BalanceMultiplier    *float64 `json:"balance_multiplier"`
	QuotaPerUnit         *float64 `json:"quota_per_unit"`
	CheckIntervalSeconds *int     `json:"check_interval_seconds"`
	Enabled              *bool    `json:"enabled"`
}

type UpstreamBalanceNotification struct {
	UpstreamID int64     `json:"upstream_id"`
	Name       string    `json:"name"`
	WebsiteURL string    `json:"website_url"`
	Balance    float64   `json:"balance"`
	Threshold  float64   `json:"threshold"`
	At         time.Time `json:"at"`
}

type UpstreamRepository interface {
	List(context.Context) ([]Upstream, error)
	Get(context.Context, int64) (*Upstream, error)
	Save(context.Context, *Upstream) (*Upstream, error)
	Delete(context.Context, int64) error
	// Claim leases a query to one server; the returned revision guards edits during IO.
	Claim(context.Context, int64, bool) (*Upstream, error)
	RecordBalance(context.Context, *Upstream, *float64, string, bool) error
}
