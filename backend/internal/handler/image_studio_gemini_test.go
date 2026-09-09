package handler

import (
	"encoding/base64"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStudioImagePlatform(t *testing.T) {
	require.Equal(t, service.PlatformOpenAI, studioImagePlatform("gpt-image-2"))
	require.Equal(t, service.PlatformGemini, studioImagePlatform("gemini-2.5-flash-image"))
	require.Equal(t, service.PlatformGemini, studioImagePlatform("models/gemini-3.1-flash-image"))
	require.Equal(t, service.PlatformGrok, studioImagePlatform("grok-imagine-image"))
	require.Equal(t, service.PlatformGrok, studioImagePlatform("grok-imagine-image-2.0"))
	require.Equal(t, "", studioImagePlatform("gpt-5.5"))
	require.Equal(t, "", studioImagePlatform("grok-imagine-video"))
}

func TestBuildStudioGeminiRequest(t *testing.T) {
	body, err := buildStudioGeminiRequest(&service.OpenAIImagesRequest{
		Prompt:      "a cat",
		AspectRatio: "16:9",
		ImageSize:   "2K",
		Uploads: []service.OpenAIImagesUpload{
			{ContentType: "image/png", Data: []byte("png")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "a cat", gjson.GetBytes(body, "contents.0.parts.0.text").String())
	require.Equal(t, "image/png", gjson.GetBytes(body, "contents.0.parts.1.inlineData.mimeType").String())
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png")), gjson.GetBytes(body, "contents.0.parts.1.inlineData.data").String())
	require.Equal(t, "16:9", gjson.GetBytes(body, "generationConfig.imageConfig.aspectRatio").String())
	require.Equal(t, "2K", gjson.GetBytes(body, "generationConfig.imageConfig.imageSize").String())
}

func TestBuildStudioGrokRequest(t *testing.T) {
	body, err := buildStudioGrokRequest(&service.OpenAIImagesRequest{
		Model:       "grok-imagine-image-2.0",
		Prompt:      "a cat",
		N:           2,
		AspectRatio: "16:9",
		ImageSize:   "2K",
		Uploads: []service.OpenAIImagesUpload{
			{ContentType: "image/png", Data: []byte("png")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "grok-imagine-image-2.0", gjson.GetBytes(body, "model").String())
	require.Equal(t, "a cat", gjson.GetBytes(body, "prompt").String())
	require.Equal(t, int64(2), gjson.GetBytes(body, "n").Int())
	require.Equal(t, "16:9", gjson.GetBytes(body, "aspect_ratio").String())
	require.Equal(t, "2k", gjson.GetBytes(body, "resolution").String())
	require.Contains(t, gjson.GetBytes(body, "image.url").String(), "data:image/png;base64,")
}

func TestStudioGeminiImagesResult(t *testing.T) {
	payload := []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"},{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]}}]}`)
	out, err := studioGeminiImagesResult(payload)
	require.NoError(t, err)
	require.Equal(t, "QUJD", gjson.GetBytes(out, "data.0.b64_json").String())
}
