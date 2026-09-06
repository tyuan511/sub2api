package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
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
	add := func(model string) {
		if strings.HasPrefix(strings.ToLower(model), "gpt-image-") && !strings.Contains(model, "*") {
			candidates[model] = struct{}{}
		}
	}
	for _, model := range openai.DefaultModels {
		add(model.ID)
	}
	for i := range accounts {
		if accounts[i].SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic) {
			for model := range accounts[i].GetModelMapping() {
				add(model)
			}
		}
	}
	models := make([]string, 0, len(candidates))
	for model := range candidates {
		for i := range accounts {
			if accounts[i].SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic) && accounts[i].IsModelSupported(model) {
				models = append(models, model)
				break
			}
		}
	}
	sort.Strings(models)
	return models, nil
}
