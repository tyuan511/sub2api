package service

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func enableOpenAIImagesUpstreamStream(body []byte, contentType string) ([]byte, string, error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "multipart/form-data") {
		return rewriteOpenAIImagesMultipartFields(body, contentType, map[string]string{"stream": "true"})
	}
	rewritten, err := sjson.SetBytes(body, "stream", true)
	return rewritten, contentType, err
}

func openAIImagesResultUnknownError() *OpenAIImagesUpstreamError {
	return &OpenAIImagesUpstreamError{
		StatusCode: http.StatusBadGateway,
		ErrorType:  "upstream_error",
		Code:       "image_generation_result_unknown",
		Message:    "Upstream image generation was interrupted; the result is unknown. Check upstream usage before retrying.",
	}
}

// Only final images enter data. Progress, partial-image previews, and duplicate
// result frames must not produce extra images or duplicate usage charges.
type openAIImagesStreamAggregate struct {
	created      int64
	images       []json.RawMessage
	sizes        []string
	seenImages   map[string]bool
	seenResults  map[string]bool
	usage        OpenAIUsage
	firstEventMS *int
	done         bool
}

func (a *openAIImagesStreamAggregate) consume(frame openAICompatSSEFrame, start time.Time) error {
	data := strings.TrimSpace(frame.Data)
	if data == "[DONE]" {
		a.done = true
		return nil
	}
	if data == "" {
		return nil
	}
	if a.firstEventMS == nil {
		ms := int(time.Since(start).Milliseconds())
		a.firstEventMS = &ms
	}
	if !gjson.Valid(data) {
		return fmt.Errorf("invalid image stream JSON")
	}
	data = openAICompatPayloadWithEventType(data, frame.EventType)
	root := gjson.Parse(data)
	if err := openAIImagesUpstreamErrorFromSSEPayload([]byte(data)); err != nil {
		return err
	}
	if upstreamError := root.Get("error"); upstreamError.IsObject() {
		return openAIImagesUpstreamErrorFromGJSON(upstreamError, "")
	}
	if a.created == 0 {
		a.created = root.Get("created").Int()
		if a.created == 0 {
			a.created = root.Get("created_at").Int()
		}
	}
	kind, object := root.Get("type").String(), root.Get("object").String()
	if strings.Contains(kind, "partial_image") || object == "image.generation.chunk" {
		return nil
	}

	var items []gjson.Result
	resultKey := ""
	perImageUsage := false
	switch {
	case object == "image.generation.result":
		items = root.Get("data").Array()
		// This provider emits one completed image job per indexed result; each
		// result's usage is local to that job, not a cumulative stream snapshot.
		if index := root.Get("index"); index.Exists() {
			resultKey = "result:" + index.Raw
		}
		perImageUsage = true
	case kind == "image_generation.completed" || kind == "image_edit.completed":
		items = []gjson.Result{root}
		if index := root.Get("image_index"); index.Exists() {
			resultKey = "image:" + index.Raw
		}
		perImageUsage = true
	case object == "" && kind == "" && root.Get("data").IsArray():
		// Some Images providers send the final regular JSON response in an SSE
		// frame, and others ignore stream=true and return JSON directly.
		items = root.Get("data").Array()
	default:
		return nil
	}
	if len(items) == 0 {
		return nil
	}
	if resultKey == "" {
		hash := sha256.Sum256([]byte(root.Get("data").Raw + root.Get("b64_json").String() + root.Get("url").String()))
		resultKey = fmt.Sprintf("content:%x", hash)
	}
	if a.seenResults == nil {
		a.seenResults = make(map[string]bool)
		a.seenImages = make(map[string]bool)
	}
	if a.seenResults[resultKey] {
		return nil
	}
	before := len(a.images)
	for i, item := range items {
		if item.Get("b64_json").String() == "" && item.Get("url").String() == "" {
			continue
		}
		identity := fmt.Sprintf("%s:%d", resultKey, i)
		if a.seenImages[identity] {
			continue
		}
		if len(a.images) >= 10 {
			return fmt.Errorf("image stream exceeds maximum image count")
		}
		// Copy only the image contract, not a whole event containing usage,
		// progress text, or provider-specific conversation metadata.
		out := make(map[string]json.RawMessage)
		for _, field := range []string{"b64_json", "url", "revised_prompt", "size", "quality", "output_format", "background"} {
			if value := item.Get(field); value.Exists() {
				out[field] = json.RawMessage(value.Raw)
			}
		}
		size := detectOpenAIImageResultSize(item.Get("b64_json").String())
		if size == "" {
			size = item.Get("size").String()
		}
		if size != "" {
			out["size"], _ = json.Marshal(size)
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return err
		}
		a.images = append(a.images, encoded)
		a.sizes = append(a.sizes, size)
		a.seenImages[identity] = true
	}
	if len(a.images) == before {
		return nil
	}
	a.seenResults[resultKey] = true
	if usage, ok := extractOpenAIUsageFromJSONBytes([]byte(data)); ok {
		if perImageUsage {
			a.usage.InputTokens += usage.InputTokens
			a.usage.ImageInputTokens += usage.ImageInputTokens
			a.usage.OutputTokens += usage.OutputTokens
			a.usage.ImageOutputTokens += usage.ImageOutputTokens
			a.usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
			a.usage.CacheReadInputTokens += usage.CacheReadInputTokens
		} else {
			mergeOpenAIUsageNonZero(&a.usage, usage)
		}
	}
	return nil
}

func readOpenAIImagesStream(body io.Reader, limit int64, start time.Time) (*openAIImagesStreamAggregate, error) {
	aggregate := &openAIImagesStreamAggregate{}
	limited := &io.LimitedReader{R: body, N: limit + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 32*1024), int(limit+1))
	var parser openAICompatSSEFrameParser
	for scanner.Scan() {
		frame, ok := parser.AddLine(strings.TrimRight(scanner.Text(), "\r"))
		if ok {
			if err := aggregate.consume(frame, start); err != nil {
				return aggregate, err
			}
		}
		if aggregate.done {
			return aggregate, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return aggregate, fmt.Errorf("read image stream: %w", err)
	}
	if limited.N == 0 {
		return aggregate, ErrUpstreamResponseBodyTooLarge
	}
	if frame, ok := parser.Finish(); ok {
		if err := aggregate.consume(frame, start); err != nil {
			return aggregate, err
		}
	}
	return aggregate, nil
}

func (s *OpenAIGatewayService) forwardOpenAIImagesAggregatedResponse(resp *http.Response, c *gin.Context, account *Account, parsed *OpenAIImagesRequest, requestModel, upstreamModel string, start time.Time) (*OpenAIForwardResult, error) {
	var aggregate *openAIImagesStreamAggregate
	var streamErr error
	if isEventStreamResponse(resp.Header) {
		aggregate, streamErr = readOpenAIImagesStream(resp.Body, resolveUpstreamResponseReadLimit(s.cfg), start)
	} else {
		aggregate = &openAIImagesStreamAggregate{done: true}
		var body []byte
		body, streamErr = readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		if streamErr == nil {
			streamErr = aggregate.consume(openAICompatSSEFrame{Data: string(body)}, start)
		}
	}
	count := len(aggregate.images)
	if streamErr == nil && (count == 0 || count < parsed.N) {
		streamErr = fmt.Errorf("image stream returned %d of %d requested images", count, parsed.N)
	}
	// A received final image remains useful even if EOF arrives before [DONE].
	// We never turn partial output into a replayable failover error.
	if streamErr != nil {
		message := sanitizeUpstreamErrorMessage(streamErr.Error())
		setOpsUpstreamError(c, http.StatusBadGateway, message, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			Kind: "image_stream_incomplete", Message: message,
		})
	}
	if count == 0 {
		// Typed image errors stop the handler's failover loop, even when the
		// status is a retryable 5xx. A new request might charge for the same job.
		upstreamErr, ok := streamErr.(*OpenAIImagesUpstreamError)
		if !ok {
			upstreamErr = openAIImagesResultUnknownError()
		}
		writeOpenAIImagesUpstreamErrorResponse(c, upstreamErr)
		return nil, upstreamErr
	}
	if aggregate.created == 0 {
		aggregate.created = start.Unix()
	}
	response := gin.H{
		"created": aggregate.created, "model": requestModel,
		"data": aggregate.images, "usage": gin.H{
			"input_tokens": aggregate.usage.InputTokens, "output_tokens": aggregate.usage.OutputTokens,
			"total_tokens":          aggregate.usage.InputTokens + aggregate.usage.OutputTokens,
			"input_tokens_details":  gin.H{"image_tokens": aggregate.usage.ImageInputTokens, "cached_tokens": aggregate.usage.CacheReadInputTokens},
			"output_tokens_details": gin.H{"image_tokens": aggregate.usage.ImageOutputTokens},
		},
	}
	if streamErr != nil {
		response["partial"] = true
		response["requested_count"] = parsed.N
		response["warning"] = gin.H{"code": "image_generation_result_unknown", "message": openAIImagesResultUnknownError().Message}
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	// SSE content length/encoding must not leak into the newly encoded JSON.
	c.Writer.Header().Del("Content-Length")
	c.Writer.Header().Del("Content-Encoding")
	c.Header("Content-Type", "application/json; charset=utf-8")
	StopOpenAIImagesJSONKeepaliveCommitted(c)
	c.JSON(http.StatusOK, response)
	result := &OpenAIForwardResult{
		RequestID: resp.Header.Get("x-request-id"), Usage: aggregate.usage,
		Model: requestModel, UpstreamModel: upstreamModel, UpstreamEndpoint: parsed.Endpoint,
		Stream: false, ResponseHeaders: resp.Header.Clone(), Duration: time.Since(start),
		FirstTokenMs: aggregate.firstEventMS, ImageCount: count,
		ImageSize: parsed.SizeTier, ImageInputSize: parsed.Size, ImageOutputSizes: aggregate.sizes,
	}
	return result, streamErr
}
