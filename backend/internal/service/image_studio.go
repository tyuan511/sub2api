package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// GetAvailableImageModels discovers image capabilities without requiring an API
// key or submitting a billed request. Empty groups must not inherit the default
// model catalog, and repository failures must not masquerade as empty groups.
func (s *GatewayService) GetAvailableImageModels(ctx context.Context, groupID int64) ([]string, error) {
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list image accounts: %w", err)
	}
	candidates := make(map[string]struct{})
	addGPT := func(model string) {
		if strings.HasPrefix(strings.ToLower(model), "gpt-image-") && !strings.Contains(model, "*") {
			candidates[model] = struct{}{}
		}
	}
	addGemini := func(model string) {
		model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
		if isImageGenerationModel(model) && !strings.Contains(model, "*") {
			candidates[model] = struct{}{}
		}
	}
	addGrok := func(model string) {
		model = strings.ToLower(strings.TrimSpace(model))
		if !isGrokImageGenerationModel(model) || strings.Contains(model, "*") || model == "grok-imagine-edit" {
			return
		}
		candidates[model] = struct{}{}
	}
	for _, model := range openai.DefaultModels {
		addGPT(model.ID)
	}
	for _, model := range gemini.DefaultModels() {
		addGemini(model.Name)
	}
	for _, model := range xai.DefaultModels() {
		addGrok(model.ID)
	}
	for i := range accounts {
		mapping := accounts[i].GetModelMapping()
		if accounts[i].SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic) {
			for model := range mapping {
				addGPT(model)
			}
		}
		if accounts[i].IsGemini() {
			for model := range mapping {
				addGemini(model)
			}
		}
		if accounts[i].IsGrok() {
			for model := range mapping {
				addGrok(model)
			}
		}
	}
	models := make([]string, 0, len(candidates))
	for model := range candidates {
		for i := range accounts {
			if studioAccountSupportsImageModel(&accounts[i], model) {
				models = append(models, model)
				break
			}
		}
	}
	sort.Strings(models)
	return models, nil
}

func studioAccountSupportsImageModel(account *Account, model string) bool {
	if account == nil || !account.IsModelSupported(model) {
		return false
	}
	if IsGPTImageGenerationModel(model) {
		return account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic)
	}
	if isImageGenerationModel(model) {
		return account.IsGemini()
	}
	if isGrokImageGenerationModel(model) {
		return account.IsGrok()
	}
	return false
}
