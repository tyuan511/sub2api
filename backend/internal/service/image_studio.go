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

var studioImagePriceTiers = []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K}

// PreviewStudioImagePrices returns per-model unit prices (multiplier=1) using the
// same order as image billing: group model pricing → group image_price_* →
// channel model pricing → built-in defaults. Token-mode cards are skipped so the
// UI can fall back to "billed by actual usage".
func (s *GatewayService) PreviewStudioImagePrices(ctx context.Context, group *Group, models []string) map[string]map[string]float64 {
	if s == nil || s.billingService == nil || group == nil || len(models) == 0 {
		return nil
	}
	out := make(map[string]map[string]float64, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		tiers := make(map[string]float64, len(studioImagePriceTiers))
		for _, tier := range studioImagePriceTiers {
			price, ok := s.previewStudioImageUnitPrice(ctx, group, model, tier)
			if !ok {
				continue
			}
			tiers[tier] = price
		}
		if len(tiers) > 0 {
			out[model] = tiers
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *GatewayService) previewStudioImageUnitPrice(ctx context.Context, group *Group, model, sizeTier string) (float64, bool) {
	sizeTier = NormalizeImageBillingTierOrDefault(sizeTier)
	gid := group.ID
	apiKey := &APIKey{GroupID: &gid, Group: group}
	var resolved *ResolvedPricing
	if s.resolver != nil {
		resolved = s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &gid, Group: group})
	}
	if resolved != nil && resolved.Source == PricingSourceGroup &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		if price, ok := studioUnifiedImageUnitPrice(s, ctx, apiKey, resolved, model, sizeTier); ok {
			return price, true
		}
	}
	if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
		return studioConfiguredImageUnitPrice(s, model, sizeTier, apiKey), true
	}
	if resolved != nil && resolved.Source == PricingSourceChannel &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		if price, ok := studioUnifiedImageUnitPrice(s, ctx, apiKey, resolved, model, sizeTier); ok {
			return price, true
		}
	}
	cost := s.billingService.CalculateImageCost(model, sizeTier, 1, imagePriceConfigFromAPIKey(apiKey), 1)
	if cost == nil || cost.TotalCost < 0 {
		return 0, false
	}
	return cost.TotalCost, true
}

func studioUnifiedImageUnitPrice(s *GatewayService, ctx context.Context, apiKey *APIKey, resolved *ResolvedPricing, model, sizeTier string) (float64, bool) {
	if s == nil || s.billingService == nil || s.resolver == nil || apiKey == nil || apiKey.Group == nil {
		return 0, false
	}
	gid := apiKey.Group.ID
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model, GroupID: &gid, Group: apiKey.Group,
		RequestCount: 1, SizeTier: sizeTier, RateMultiplier: 1,
		Resolver: s.resolver, Resolved: resolved,
	})
	if err != nil || cost == nil || cost.TotalCost < 0 {
		return 0, false
	}
	return cost.TotalCost, true
}

func studioConfiguredImageUnitPrice(s *GatewayService, model, sizeTier string, apiKey *APIKey) float64 {
	cost := s.billingService.CalculateImageCost(model, sizeTier, 1, imagePriceConfigFromAPIKey(apiKey), 1)
	if cost == nil {
		return 0
	}
	return cost.TotalCost
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
