package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/tidwall/gjson"
)

func studioImagePlatform(model string) string {
	if service.IsGeminiImageGenerationModel(model) {
		return service.PlatformGemini
	}
	if service.IsGrokImageGenerationModel(model) {
		return service.PlatformGrok
	}
	if service.IsGPTImageGenerationModel(model) {
		return service.PlatformOpenAI
	}
	return ""
}

func studioMaxReferenceImages(model string) int {
	switch studioImagePlatform(model) {
	case service.PlatformGemini:
		if strings.Contains(strings.ToLower(model), "pro-image") {
			return 14
		}
		return 3
	case service.PlatformGrok:
		return 3
	case service.PlatformOpenAI:
		return 8
	default:
		return 0
	}
}

func grokStudioResolution(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1k":
		return "1k"
	case "2k", "4k":
		return "2k"
	default:
		return ""
	}
}

func buildStudioGrokRequest(parsed *service.OpenAIImagesRequest) ([]byte, error) {
	if parsed == nil {
		return nil, fmt.Errorf("missing grok image request")
	}
	payload := map[string]any{
		"model":  parsed.Model,
		"prompt": parsed.Prompt,
		"n":      parsed.N,
	}
	if ratio := strings.TrimSpace(parsed.AspectRatio); ratio != "" && !strings.EqualFold(ratio, "auto") {
		payload["aspect_ratio"] = ratio
	}
	if size := grokStudioResolution(parsed.ImageSize); size != "" {
		payload["resolution"] = size
	}
	images := make([]map[string]string, 0, len(parsed.Uploads))
	for _, upload := range parsed.Uploads {
		if len(upload.Data) == 0 {
			continue
		}
		mime := strings.TrimSpace(upload.ContentType)
		if mime == "" {
			mime = "image/png"
		}
		images = append(images, map[string]string{
			"url":  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(upload.Data),
			"type": "image_url",
		})
	}
	if len(images) == 1 {
		payload["image"] = images[0]
	} else if len(images) > 1 {
		payload["image"] = images[0]
		payload["images"] = images
	}
	return json.Marshal(payload)
}

func buildStudioGeminiRequest(parsed *service.OpenAIImagesRequest) ([]byte, error) {
	if parsed == nil {
		return nil, fmt.Errorf("missing gemini image request")
	}
	parts := make([]map[string]any, 0, 1+len(parsed.Uploads))
	if prompt := strings.TrimSpace(parsed.Prompt); prompt != "" {
		parts = append(parts, map[string]any{"text": prompt})
	}
	for _, upload := range parsed.Uploads {
		if len(upload.Data) == 0 {
			continue
		}
		mime := strings.TrimSpace(upload.ContentType)
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"mimeType": mime,
				"data":     base64.StdEncoding.EncodeToString(upload.Data),
			},
		})
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("prompt is required")
	}
	imageConfig := map[string]any{}
	if ratio := strings.TrimSpace(parsed.AspectRatio); ratio != "" && !strings.EqualFold(ratio, "auto") {
		imageConfig["aspectRatio"] = ratio
	}
	if size := strings.TrimSpace(parsed.ImageSize); size != "" {
		imageConfig["imageSize"] = size
	}
	generationConfig := map[string]any{
		"responseModalities": []string{"TEXT", "IMAGE"},
	}
	if len(imageConfig) > 0 {
		generationConfig["imageConfig"] = imageConfig
	}
	return json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": parts},
		},
		"generationConfig": generationConfig,
	})
}

func studioGeminiImagesResult(payload []byte) ([]byte, error) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return nil, fmt.Errorf("invalid gemini image response")
	}
	images := make([]map[string]string, 0, 1)
	gjson.GetBytes(payload, "candidates").ForEach(func(_, candidate gjson.Result) bool {
		candidate.Get("content.parts").ForEach(func(_, part gjson.Result) bool {
			inline := part.Get("inlineData")
			if !inline.Exists() {
				inline = part.Get("inline_data")
			}
			data := strings.TrimSpace(inline.Get("data").String())
			if data == "" {
				return true
			}
			mime := inline.Get("mimeType")
			if !mime.Exists() {
				mime = inline.Get("mime_type")
			}
			mimeType := strings.ToLower(strings.TrimSpace(mime.String()))
			if mimeType != "" && !strings.HasPrefix(mimeType, "image/") {
				return true
			}
			images = append(images, map[string]string{"b64_json": data})
			return true
		})
		return true
	})
	if len(images) == 0 {
		return nil, fmt.Errorf("empty_result")
	}
	return json.Marshal(map[string]any{"data": images})
}

func studioGeminiModelAction(model string) string {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	return model + ":generateContent"
}
