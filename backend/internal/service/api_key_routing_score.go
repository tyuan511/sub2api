package service

import (
	"strings"
)

// NormalizeAPIKeyRoutingModelFamily keeps score-cardinality bounded while
// retaining the product families whose behavior and pricing are meaningfully
// different. Unknown names share a platform baseline instead of becoming an
// attacker-controlled Redis/metrics dimension.
func NormalizeAPIKeyRoutingModelFamily(platform, model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(value, "/models/"); index >= 0 {
		value = value[index+len("/models/"):]
	}
	value = strings.TrimPrefix(value, "models/")
	families := []struct {
		needle string
		name   string
	}{
		{"claude-opus", "claude-opus"},
		{"claude-sonnet", "claude-sonnet"},
		{"claude-haiku", "claude-haiku"},
		{"gpt-5", "gpt-5"},
		{"gpt-4.1", "gpt-4.1"},
		{"gpt-4o", "gpt-4o"},
		{"o4-", "o4"},
		{"o3-", "o3"},
		{"gemini-3", "gemini-3"},
		{"gemini-2.5", "gemini-2.5"},
		{"gemini-2.0", "gemini-2.0"},
		{"grok-4", "grok-4"},
		{"grok-3", "grok-3"},
	}
	for _, family := range families {
		if strings.Contains(value, family.needle) {
			return family.name
		}
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = "unknown"
	}
	return platform + "-other"
}

// NormalizeAPIKeyRoutingEndpointKind converts concrete route paths into a
// small enum suitable for score snapshots and breaker diagnostics.
func NormalizeAPIKeyRoutingEndpointKind(path string) string {
	value := strings.ToLower(strings.TrimSpace(path))
	switch value {
	case "messages", "chat_completions", "responses", "embeddings", "images", "video", "audio", "live", "generate_content", "count_tokens", "other":
		return value
	}
	switch {
	case strings.Contains(value, "counttokens") || strings.Contains(value, "count_tokens"):
		return "count_tokens"
	case strings.Contains(value, "streamgeneratecontent") || strings.Contains(value, "generatecontent"):
		return "generate_content"
	case strings.Contains(value, "chat/completions"):
		return "chat_completions"
	case strings.Contains(value, "responses"):
		return "responses"
	case strings.Contains(value, "embeddings"):
		return "embeddings"
	case strings.Contains(value, "images"):
		return "images"
	case strings.Contains(value, "video"):
		return "video"
	case strings.Contains(value, "audio"):
		return "audio"
	case strings.Contains(value, "live") || strings.Contains(value, "realtime"):
		return "live"
	case strings.Contains(value, "messages"):
		return "messages"
	default:
		return "other"
	}
}
