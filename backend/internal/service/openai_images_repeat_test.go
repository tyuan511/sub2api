package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardOpenAIImagesAPIKey_RepeatsN1ForMultiImage(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2.5-flare","prompt":"cat","n":3,"size":"1024x1024"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	responses := make([]*http.Response, 3)
	for i := range responses {
		responses[i] = &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				fmt.Sprintf(`{"created":1,"data":[{"b64_json":"aW1n%d"}]}`, i+1),
			)),
		}
	}
	upstream := &httpUpstreamRecorder{responses: responses}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.Equal(t, 3, parsed.N)

	result, err := svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 3, result.ImageCount)
	require.Len(t, upstream.bodies, 3)
	for _, sent := range upstream.bodies {
		require.Equal(t, int64(1), gjson.GetBytes(sent, "n").Int())
	}
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 3)
	require.Equal(t, "aW1n1", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "aW1n3", gjson.Get(rec.Body.String(), "data.2.b64_json").String())
}
