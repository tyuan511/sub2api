package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func imageStreamResult(index int, image string, input, output int) string {
	return fmt.Sprintf("data: {\"object\":\"image.generation.result\",\"index\":%d,\"total\":4,\"created\":1710000000,\"data\":[{\"b64_json\":%q,\"revised_prompt\":\"original\"}],\"usage\":{\"input_tokens\":%d,\"output_tokens\":%d,\"output_tokens_details\":{\"image_tokens\":%d}}}\n\n", index, image, input, output, output)
}

func streamImageAccount() *Account {
	account := newOpenAIImagesAPIKeyAccount()
	account.Extra = map[string]any{"openai_images_upstream_stream": true}
	return account
}

func TestImageUpstreamStream_AggregatesFourResultsWithoutDuplicateBilling(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"original","n":4,"size":"3840x2160"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	var stream strings.Builder
	stream.WriteString(": keepalive\r\n\r\ndata: {\"object\":\"image.generation.chunk\",\"data\":[]}\n\n")
	for i := 1; i <= 4; i++ {
		// Identical bytes are allowed for distinct result indexes.
		frame := imageStreamResult(i, "aW1hZ2U=", i, i*10)
		stream.WriteString(frame)
		stream.WriteString(frame)
	}
	stream.WriteString("data: [DONE]\n\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream.String()))}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, streamImageAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, int64(4), gjson.GetBytes(upstream.lastBody, "n").Int())
	require.False(t, parsed.Stream, "caller contract must remain non-streaming")
	require.False(t, result.Stream)
	require.Equal(t, 4, result.ImageCount)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 100, result.Usage.OutputTokens)
	require.Equal(t, 100, result.Usage.ImageOutputTokens)
	require.Len(t, gjson.GetBytes(rec.Body.Bytes(), "data").Array(), 4)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.NotNil(t, result.FirstTokenMs)
	require.True(t, gjson.ValidBytes(rec.Body.Bytes()))
	require.NotContains(t, rec.Body.String(), "image.generation.chunk")
}

func TestImageUpstreamStream_OptInAndClientStreamContracts(t *testing.T) {
	for _, tt := range []struct {
		name   string
		flag   any
		stream bool
	}{
		{"missing", nil, false}, {"disabled", false, false}, {"wrong type", "true", false}, {"client SSE", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2","prompt":"test","stream":%t}`, tt.stream))
			c, rec := newOpenAIImagesTestContext(t, body)
			upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
			if tt.stream {
				upstream.resp.Header.Set("Content-Type", "text/event-stream")
				upstream.resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"aW1hZ2U=\"}\n\ndata: [DONE]\n\n"))
			}
			svc := newOpenAIImagesTestService(upstream)
			account := streamImageAccount()
			account.Extra["openai_images_upstream_stream"] = tt.flag
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			_, err = svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, tt.stream, gjson.GetBytes(upstream.lastBody, "stream").Bool())
			if tt.stream {
				require.Contains(t, rec.Body.String(), "data: [DONE]")
			}
		})
	}
}

func TestImageUpstreamStream_PreservesMultipartFilesAndControls(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range map[string]string{"model": "gpt-image-2", "prompt": "keep verbatim", "n": "4", "stream": "false", "size": "3840x2160", "input_fidelity": "high"} {
		require.NoError(t, w.WriteField(name, value))
	}
	for _, name := range []string{"image[]", "mask"} {
		part, err := w.CreateFormFile(name, name+".png")
		require.NoError(t, err)
		_, err = part.Write([]byte{0, 1, 2, 3, 255})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	rewritten, contentType, err := enableOpenAIImagesUpstreamStream(body.Bytes(), w.FormDataContentType())
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/images/edits", bytes.NewReader(rewritten))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	require.NoError(t, req.ParseMultipartForm(1<<20))
	defer req.MultipartForm.RemoveAll()
	require.Equal(t, []string{"true"}, req.MultipartForm.Value["stream"])
	require.Equal(t, "4", req.FormValue("n"))
	require.Equal(t, "keep verbatim", req.FormValue("prompt"))
	require.Equal(t, "high", req.FormValue("input_fidelity"))
	for _, name := range []string{"image[]", "mask"} {
		file, _, err := req.FormFile(name)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		file.Close()
		require.NoError(t, err)
		require.Equal(t, []byte{0, 1, 2, 3, 255}, data)
	}
}

func TestImageUpstreamStream_FinalEventsExcludePreviews(t *testing.T) {
	stream := "event: image_generation.partial_image\ndata: {\"b64_json\":\"cHJldmlldw==\"}\n\n" +
		"event: image_generation.completed\ndata: {\"b64_json\":\"ZmluYWw=\",\ndata: \"usage\":{\"input_tokens\":2,\"output_tokens\":3}}\n\n" +
		"data: [DONE]\n\n"
	a, err := readOpenAIImagesStream(strings.NewReader(stream), 1<<20, time.Now())
	require.NoError(t, err)
	require.Len(t, a.images, 1)
	require.Equal(t, "ZmluYWw=", gjson.GetBytes(a.images[0], "b64_json").String())
	require.Equal(t, 3, a.usage.OutputTokens)
}

func TestImageUpstreamStream_PartialAndUnknownNeverBecomeFailover(t *testing.T) {
	for _, tt := range []struct {
		name  string
		body  io.ReadCloser
		count int
	}{
		{"short clean EOF", io.NopCloser(strings.NewReader(imageStreamResult(1, "aW1hZ2U=", 2, 20))), 1},
		{"read failure after result", io.NopCloser(io.MultiReader(strings.NewReader(imageStreamResult(1, "aW1hZ2U=", 2, 20)), &openAIImagesReadErrorBody{err: io.ErrUnexpectedEOF})), 1},
		{"malformed event after result", io.NopCloser(strings.NewReader(imageStreamResult(1, "aW1hZ2U=", 2, 20) + "data: {broken\n\n")), 1},
		{"empty EOF", io.NopCloser(strings.NewReader(": heartbeat\n\n")), 0},
		{"explicit error", io.NopCloser(strings.NewReader("event: error\ndata: {\"error\":{\"type\":\"server_error\",\"message\":\"failed\"}}\n\n")), 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"test","n":4}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			svc := newOpenAIImagesTestService(nil)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: tt.body}
			result, err := svc.forwardOpenAIImagesAggregatedResponse(resp, c, streamImageAccount(), parsed, "gpt-image-2", "gpt-image-2", time.Now())
			require.Error(t, err)
			var retry *UpstreamFailoverError
			require.False(t, errors.As(err, &retry))
			if tt.count > 0 {
				require.Equal(t, tt.count, result.ImageCount)
				require.Equal(t, 20, result.Usage.OutputTokens)
				require.Equal(t, 200, rec.Code)
				require.True(t, gjson.GetBytes(rec.Body.Bytes(), "partial").Bool())
				require.Len(t, gjson.GetBytes(rec.Body.Bytes(), "data").Array(), tt.count)
			} else {
				require.Nil(t, result)
				require.Equal(t, 502, rec.Code)
				require.True(t, gjson.GetBytes(rec.Body.Bytes(), "error").Exists())
			}
		})
	}
}

func TestImageUpstreamStream_HTTP524IsResultUnknown(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"test"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 524, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("error code: 524"))}}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, streamImageAccount(), body, parsed, "")
	require.Error(t, err)
	require.Nil(t, result)
	var retry *UpstreamFailoverError
	require.False(t, errors.As(err, &retry))
	require.Equal(t, "image_generation_result_unknown", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
}

func TestImageUpstreamStream_BoundsInputAndKeepsAlreadyReceivedImages(t *testing.T) {
	first := imageStreamResult(1, "aW1hZ2U=", 2, 20)
	stream := first + "data: " + strings.Repeat("x", 2000) + "\n\n"
	a, err := readOpenAIImagesStream(strings.NewReader(stream), int64(len(first)+100), time.Now())
	require.Error(t, err)
	require.Len(t, a.images, 1)
}

func TestImageUpstreamStream_DetachedRequestHasExecutionDeadline(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"test"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	want, _ := ctx.Deadline()
	cancel()
	_, err = svc.ForwardImages(ctx, c, streamImageAccount(), body, parsed, "")
	require.NoError(t, err)
	got, ok := upstream.lastReq.Context().Deadline()
	require.True(t, ok)
	require.Equal(t, want, got)
}

func TestImageUpstreamStream_JSONFallbackAndActualDimensions(t *testing.T) {
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 32, 16))))
	encoded := base64.StdEncoding.EncodeToString(imageBytes.Bytes())
	stream := fmt.Sprintf("data: {\"data\":[{\"b64_json\":%q,\"size\":\"1536x1024\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}\n\ndata: [DONE]\n\n", encoded)
	a, err := readOpenAIImagesStream(strings.NewReader(stream), 1<<20, time.Now())
	require.NoError(t, err)
	require.Equal(t, []string{"32x16"}, a.sizes)
	require.Equal(t, "32x16", gjson.GetBytes(a.images[0], "size").String())
}

func TestImageUpstreamStream_PartialImagesSurviveAsyncStorageAndPolling(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"test","n":4}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	svc := newOpenAIImagesTestService(nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	stream := imageStreamResult(1, base64.StdEncoding.EncodeToString(pngBytes), 2, 20)
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	result, err := svc.forwardOpenAIImagesAggregatedResponse(resp, c, streamImageAccount(), parsed, parsed.Model, parsed.Model, time.Now())
	require.Error(t, err)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 200, rec.Code)

	storage := &fakeImageStorage{}
	tasks := NewImageTaskServiceWithUploader(&imageTaskMemoryStore{}, NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := tasks.Create(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, tasks.Complete(context.Background(), task.ID, rec.Code, rec.Body.Bytes()))
	completed, err := tasks.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Len(t, storage.saved, 1)
	require.Equal(t, pngBytes, storage.saved[0].data)
	require.NotEmpty(t, completed.ImageURL)
	require.NotContains(t, string(completed.Result), "b64_json")
	require.True(t, gjson.GetBytes(completed.Result, "partial").Bool())
	require.Equal(t, int64(4), gjson.GetBytes(completed.Result, "requested_count").Int())
	require.Equal(t, "image_generation_result_unknown", gjson.GetBytes(completed.Result, "warning.code").String())
	require.Equal(t, int64(20), gjson.GetBytes(completed.Result, "usage.output_tokens").Int())
}
