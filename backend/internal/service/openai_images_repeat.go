package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const openAIImagesRepeatMax = 10

func (s *OpenAIGatewayService) forwardOpenAIImagesAPIKeyRepeat(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	contentType string,
	parsed *OpenAIImagesRequest,
	requestModel, upstreamModel string,
	start time.Time,
) (*OpenAIForwardResult, error) {
	wanted := parsed.N
	if wanted < 1 {
		wanted = 1
	}
	if wanted > openAIImagesRepeatMax {
		wanted = openAIImagesRepeatMax
	}
	singleBody, singleType, err := rewriteOpenAIImagesN(body, contentType, 1)
	if err != nil {
		return nil, err
	}

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()

	token, _, err := s.GetAccessToken(upstreamCtx, account)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	logger.L().Info("openai.images.repeat_n1",
		zap.Int64("account_id", account.ID),
		zap.String("account", account.Name),
		zap.String("model", requestModel),
		zap.Int("requested", wanted),
	)

	var (
		images     []json.RawMessage
		sizes      []string
		usage      OpenAIUsage
		created    int64
		lastHeader http.Header
		lastID     string
	)
	singleParsed := *parsed
	singleParsed.N = 1

	for len(images) < wanted {
		resp, callErr := s.doOpenAIImagesAPIKeyCall(upstreamCtx, c, account, singleBody, singleType, token, proxyURL, parsed.Endpoint)
		if callErr != nil {
			if len(images) == 0 {
				safeErr := sanitizeUpstreamErrorMessage(callErr.Error())
				setOpsUpstreamError(c, 0, safeErr, "")
				return nil, fmt.Errorf("upstream request failed: %s", safeErr)
			}
			logger.L().Warn("openai.images.repeat_partial", zap.Int("received", len(images)), zap.Int("requested", wanted), zap.Error(callErr))
			break
		}
		if resp.StatusCode >= 400 {
			respBody := s.readUpstreamErrorBody(resp)
			_ = resp.Body.Close()
			respBody = s.redactAgentIdentitySensitiveBody(upstreamCtx, account, respBody)
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
			if len(images) == 0 {
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
				if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
					shouldDisable := s.handleFailoverSideEffects(upstreamCtx, resp, account, respBody, upstreamModel)
					retryableOnSameAccount := !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode)
					if isOpenAIHTTPUpstreamAccessStateError(resp.StatusCode, upstreamMsg, respBody) {
						return nil, newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg, retryableOnSameAccount)
					}
					return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody, RetryableOnSameAccount: retryableOnSameAccount}
				}
				return s.handleOpenAIImagesErrorResponse(upstreamCtx, resp, c, account, upstreamModel)
			}
			logger.L().Warn("openai.images.repeat_partial", zap.Int("received", len(images)), zap.Int("requested", wanted), zap.Int("status", resp.StatusCode))
			break
		}

		respBody, readErr := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
		_ = resp.Body.Close()
		if readErr != nil {
			if len(images) == 0 {
				return nil, readErr
			}
			logger.L().Warn("openai.images.repeat_partial", zap.Int("received", len(images)), zap.Int("requested", wanted), zap.Error(readErr))
			break
		}
		if isEventStreamResponse(resp.Header) {
			agg, streamErr := readOpenAIImagesStream(bytes.NewReader(respBody), resolveUpstreamResponseReadLimit(s.cfg), start)
			if streamErr != nil && len(agg.images) == 0 && len(images) == 0 {
				unknown := openAIImagesResultUnknownError()
				writeOpenAIImagesUpstreamErrorResponse(c, unknown)
				return nil, unknown
			}
			images = append(images, agg.images...)
			sizes = append(sizes, agg.sizes...)
			usage.InputTokens += agg.usage.InputTokens
			usage.OutputTokens += agg.usage.OutputTokens
			usage.ImageInputTokens += agg.usage.ImageInputTokens
			usage.ImageOutputTokens += agg.usage.ImageOutputTokens
			if created == 0 {
				created = agg.created
			}
			lastHeader = resp.Header.Clone()
			lastID = resp.Header.Get("x-request-id")
			if len(agg.images) == 0 {
				break
			}
			continue
		}

		respBody = expandOpenAIImagesAPIData(respBody)
		respBody = s.backfillOpenAIImagesB64JSON(upstreamCtx, account, &singleParsed, respBody)
		batch := gjson.GetBytes(respBody, "data").Array()
		got := 0
		for _, item := range batch {
			if !item.IsObject() {
				continue
			}
			if strings.TrimSpace(item.Get("b64_json").String()) == "" && strings.TrimSpace(item.Get("url").String()) == "" {
				continue
			}
			images = append(images, json.RawMessage(item.Raw))
			got++
		}
		sizes = append(sizes, collectOpenAIResponseImageOutputSizesFromJSONBytes(respBody)...)
		if part, ok := extractOpenAIUsageFromJSONBytes(respBody); ok {
			usage.InputTokens += part.InputTokens
			usage.OutputTokens += part.OutputTokens
			usage.ImageInputTokens += part.ImageInputTokens
			usage.ImageOutputTokens += part.ImageOutputTokens
			usage.CacheCreationInputTokens += part.CacheCreationInputTokens
			usage.CacheReadInputTokens += part.CacheReadInputTokens
		}
		if created == 0 {
			created = gjson.GetBytes(respBody, "created").Int()
		}
		lastHeader = resp.Header.Clone()
		lastID = resp.Header.Get("x-request-id")
		if got == 0 {
			break
		}
	}

	if len(images) == 0 {
		unknown := openAIImagesResultUnknownError()
		writeOpenAIImagesUpstreamErrorResponse(c, unknown)
		return nil, unknown
	}
	if created == 0 {
		created = start.Unix()
	}
	if lastHeader != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), lastHeader, s.responseHeaderFilter)
	}
	c.Writer.Header().Del("Content-Length")
	c.Header("Content-Type", "application/json; charset=utf-8")
	StopOpenAIImagesJSONKeepaliveCommitted(c)
	response := gin.H{
		"created": created, "model": requestModel,
		"data": images, "usage": gin.H{
			"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
			"total_tokens":          usage.InputTokens + usage.OutputTokens,
			"input_tokens_details":  gin.H{"image_tokens": usage.ImageInputTokens, "cached_tokens": usage.CacheReadInputTokens},
			"output_tokens_details": gin.H{"image_tokens": usage.ImageOutputTokens},
		},
	}
	if len(images) < wanted {
		response["partial"] = true
		response["requested_count"] = wanted
	}
	c.JSON(http.StatusOK, response)
	return &OpenAIForwardResult{
		RequestID: lastID, Usage: usage, UpstreamHeaders: lastHeader,
		Model: requestModel, UpstreamModel: upstreamModel, UpstreamEndpoint: parsed.Endpoint,
		Stream: false, ResponseHeaders: lastHeader, Duration: time.Since(start),
		ImageCount: len(images), ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size, ImageOutputSizes: sizes,
	}, nil
}

func (s *OpenAIGatewayService) doOpenAIImagesAPIKeyCall(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	contentType, token, proxyURL, endpoint string,
) (*http.Response, error) {
	req, err := s.buildOpenAIImagesRequest(ctx, c, account, body, contentType, token, endpoint)
	if err != nil {
		return nil, err
	}
	return s.doOpenAIUpstream(req, proxyURL, account)
}
