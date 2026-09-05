package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type imageStreamLiveUpstream struct {
	HTTPUpstream
	client  *http.Client
	calls   int
	capture io.Writer
}

type imageStreamLiveBody struct {
	io.Reader
	io.Closer
}

func (u *imageStreamLiveUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	req.Header.Set("User-Agent", "Sub2API-Diagnostics/1.0")
	resp, err := u.client.Do(req)
	if err == nil && u.capture != nil {
		resp.Body = imageStreamLiveBody{Reader: io.TeeReader(resp.Body, u.capture), Closer: resp.Body}
	}
	return resp, err
}

// This explicit opt-in test makes exactly one billed four-image request.
// Ordinary test runs never contact a provider or need credentials.
func TestImageUpstreamStream_LiveProvider(t *testing.T) {
	key := os.Getenv("SUB2API_TEST_IMAGE_UPSTREAM_KEY")
	base := os.Getenv("SUB2API_TEST_IMAGE_UPSTREAM_URL")
	if key == "" || base == "" || os.Getenv("SUB2API_TEST_IMAGE_BILLED_REQUEST") != "four-images" {
		t.Skip("billed image provider test not enabled")
	}
	dir := os.Getenv("SUB2API_TEST_IMAGE_OUTPUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	require.NoError(t, os.MkdirAll(dir, 0700))
	capture, err := os.OpenFile(filepath.Join(dir, "upstream.sse"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	require.NoError(t, err)
	defer capture.Close()
	body := []byte(`{"model":"gpt-image-2","prompt":"Diagnostic image: a single blue circle centered on a plain light gray background. No text.","n":4,"size":"3840x2160"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	u := &imageStreamLiveUpstream{client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	u.capture = capture
	svc := newOpenAIImagesTestService(u)
	account := streamImageAccount()
	account.Credentials["api_key"] = key
	account.Credentials["base_url"] = base
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	if err != nil {
		t.Fatalf("live provider returned %T (HTTP %d)", err, rec.Code)
	}
	require.Equal(t, 1, u.calls)
	require.Equal(t, 200, rec.Code)
	require.False(t, result.Stream)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	for i, item := range gjson.GetBytes(rec.Body.Bytes(), "data").Array() {
		data, decodeErr := base64.StdEncoding.DecodeString(item.Get("b64_json").String())
		require.NoError(t, decodeErr)
		require.NoError(t, os.WriteFile(filepath.Join(dir, string(rune('1'+i))+".png"), data, 0600))
	}
	summary := map[string]any{"http_status": rec.Code, "image_count": result.ImageCount, "duration_ms": result.Duration.Milliseconds(), "first_event_ms": result.FirstTokenMs, "sizes": result.ImageOutputSizes, "usage": result.Usage, "request_id": result.RequestID, "upstream_calls": u.calls}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "summary.json"), encoded, 0600))
	t.Log(string(encoded))
	require.Equal(t, 4, result.ImageCount)
}
