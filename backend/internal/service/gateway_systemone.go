package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/gin-gonic/gin"
)

type SystemOneForwardResult struct {
	ForwardResult
	StatusCode  int
	Body        []byte
	ContentType string
}

type SystemOneUpstreamError struct {
	StatusCode int
}

const TypeSafeCredentialRejectedReason GatewayFailureReason = "typesafe_api_key_rejected"

func (e *SystemOneUpstreamError) Error() string {
	return fmt.Sprintf("typesafe upstream rejected request with status %d", e.StatusCode)
}

func (s *GatewayService) ForwardSystemOne(ctx context.Context, c *gin.Context, account *Account, body []byte) (*SystemOneForwardResult, error) {
	started := time.Now()
	if account == nil || !account.IsTypeSafe() || account.Type != AccountTypeAPIKey {
		return nil, errors.New("invalid typesafe account")
	}
	model := typesafe.JevLatestModel
	modelPresent := false
	var envelope struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Model) != "" {
		model = strings.TrimSpace(envelope.Model)
		modelPresent = true
	}
	mappedModel := account.GetMappedModel(model)
	upstreamBody := body
	if modelPresent {
		upstreamBody = ReplaceModelInBody(body, mappedModel)
	}
	key := account.GetTypeSafeAPIKey()
	if key == "" {
		return nil, errors.New("typesafe api key is missing")
	}
	baseURL, err := s.validateUpstreamBaseURL(account.GetTypeSafeBaseURL())
	if err != nil {
		return nil, err
	}
	req, err := typesafe.NewSystemOneRequest(ctx, baseURL, key, upstreamBody)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, s.handleUpstreamTransportError(ctx, c, account, err, OpsUpstreamErrorEvent{
			Passthrough: true,
			UpstreamURL: req.URL.Scheme + "://" + req.URL.Host + req.URL.Path,
		})
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity {
			return nil, &SystemOneUpstreamError{StatusCode: resp.StatusCode}
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 || resp.StatusCode >= 500 {
			if s.rateLimitService != nil {
				s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, nil, mappedModel)
			}
			failoverErr := &UpstreamFailoverError{
				StatusCode:       resp.StatusCode,
				ResponseHeaders:  resp.Header.Clone(),
				ClientStatusCode: http.StatusBadGateway,
				ClientMessage:    "TypeSafe upstream request failed",
			}
			if resp.StatusCode == http.StatusUnauthorized {
				failoverErr.Stage = GatewayFailureStageAccountAuth
				failoverErr.Scope = GatewayFailureScopeAccount
				failoverErr.Reason = TypeSafeCredentialRejectedReason
				failoverErr.NextAccountAction = NextAccountRetry
			}
			return nil, failoverErr
		}
		return nil, &SystemOneUpstreamError{StatusCode: http.StatusBadGateway}
	}

	decoded, err := typesafe.DecodeSystemOneResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	// The body has been validated as JSON; do not reflect an upstream content
	// type such as text/html back to clients. Keep the public response model on
	// the requested alias while retaining the actual upstream model for billing
	// and diagnostics.
	contentType := "application/json"
	responseBody := decoded.Body
	if strings.TrimSpace(decoded.Model) != "" {
		responseBody = ReplaceModelInBody(responseBody, model)
	}
	return &SystemOneForwardResult{
		ForwardResult: ForwardResult{
			RequestID:             resp.Header.Get("x-request-id"),
			UpstreamHeaders:       resp.Header.Clone(),
			Usage:                 ClaudeUsage{InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens},
			Model:                 model,
			UpstreamModel:         mappedModel,
			UpstreamResponseModel: decoded.Model,
			Duration:              time.Since(started),
		},
		StatusCode:  resp.StatusCode,
		Body:        responseBody,
		ContentType: contentType,
	}, nil
}
