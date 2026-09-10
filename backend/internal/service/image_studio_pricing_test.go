package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPreviewStudioImagePricesFollowsBillingOrder(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	svc := &GatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	groupPrice := 0.021
	modelPrice := 0.99
	group := &Group{
		ID:           1,
		ImagePrice2K: &groupPrice,
		ModelPricing: []ChannelModelPricing{{
			Models:      []string{"gpt-image-2"},
			BillingMode: BillingModeImage,
			Intervals:   []PricingInterval{{TierLabel: "2K", PerRequestPrice: &modelPrice}},
		}},
	}

	prices := svc.PreviewStudioImagePrices(context.Background(), group, []string{"gpt-image-2"})
	require.InDelta(t, 0.99, prices["gpt-image-2"]["2K"], 1e-12)

	group.ModelPricing = nil
	prices = svc.PreviewStudioImagePrices(context.Background(), group, []string{"gpt-image-2"})
	require.InDelta(t, 0.021, prices["gpt-image-2"]["2K"], 1e-12)
}

func TestPreviewStudioImagePricesUsesBuiltInDefault(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	svc := &GatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	prices := svc.PreviewStudioImagePrices(context.Background(), &Group{ID: 2}, []string{"gpt-image-2"})
	require.InDelta(t, defaultImageGenerationPrice*1.5, prices["gpt-image-2"]["2K"], 1e-12)
}

func TestPreviewStudioImagePricesNilSafe(t *testing.T) {
	require.Nil(t, (*GatewayService)(nil).PreviewStudioImagePrices(context.Background(), &Group{ID: 1}, []string{"gpt-image-2"}))
	require.Nil(t, (&GatewayService{}).PreviewStudioImagePrices(context.Background(), &Group{ID: 1}, []string{"gpt-image-2"}))
}
