package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type imageStudioUserRepo struct{ service.UserRepository }

func TestStudioDimensions(t *testing.T) {
	for _, tt := range []struct{ size, ratio, tier string }{
		{"1024x1024", "1:1", "1K"}, {"2016x864", "21:9", "2K"},
		{"3024x1296", "21:9", "4K"}, {"3840x2160", "16:9", "4K"},
		{"768x1024", "3:4", "1K"}, {"auto", "auto", "1K"}, {"", "auto", "1K"},
		{"1792x1008", "16:9", "1K"}, {"1008x1792", "9:16", "1K"}, {"1792x768", "21:9", "1K"},
	} {
		t.Run(tt.size, func(t *testing.T) {
			ratio, tier := studioDimensions(tt.size)
			require.Equal(t, tt.ratio, ratio)
			require.Equal(t, tt.tier, tier)
		})
	}
}

func (imageStudioUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 42}, nil
}

type imageStudioGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r imageStudioGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

type imageStudioSubscriptionRepo struct {
	service.UserSubscriptionRepository
}

func (imageStudioSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

type imageStudioFailingAccountRepo struct{ service.AccountRepository }

func (imageStudioFailingAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return nil, errors.New("repository unavailable")
}

func TestImageGenerationGroups(t *testing.T) {
	imageAccount := service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-image-2": "gpt-image-2", "gpt-5.5": "gpt-5.5"}}}
	textAccount := imageAccount
	textAccount.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}}
	defaultsAccount := imageAccount
	defaultsAccount.Credentials = nil
	wildcardAccount := imageAccount
	wildcardAccount.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-image-*": "gpt-image-2"}}
	otherPlatformAccount := imageAccount
	otherPlatformAccount.Platform = service.PlatformGrok
	group := service.Group{ID: 1, Name: "Images", Status: service.StatusActive, Platform: service.PlatformOpenAI, AllowImageGeneration: true}
	for _, tt := range []struct {
		name          string
		accounts      []service.Account
		changeGroup   func(*service.Group)
		unauthorized  bool
		repositoryErr bool
		wantStatus    int
		wantModels    []string
	}{
		{name: "image and text models", accounts: []service.Account{imageAccount}, wantModels: []string{"gpt-image-2"}},
		{name: "text only despite image permission", accounts: []service.Account{textAccount}},
		{name: "no accounts must not inherit defaults"},
		{name: "wrong account platform", accounts: []service.Account{otherPlatformAccount}},
		{name: "unrestricted account", accounts: []service.Account{defaultsAccount}, wantModels: []string{"gpt-image-1", "gpt-image-1.5", "gpt-image-2"}},
		{name: "wildcards become concrete models", accounts: []service.Account{wildcardAccount}, wantModels: []string{"gpt-image-1", "gpt-image-1.5", "gpt-image-2"}},
		{name: "disabled image permission", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) { g.AllowImageGeneration = false }},
		{name: "inactive group", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) { g.Status = "inactive" }},
		{name: "other group platform", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) { g.Platform = service.PlatformGrok }},
		{name: "exclusive group access denied", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) { g.IsExclusive = true }},
		{name: "subscription required", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) { g.SubscriptionType = "subscription" }},
		{name: "custom list hides images", accounts: []service.Account{imageAccount}, changeGroup: func(g *service.Group) {
			g.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5.5"}}
		}},
		{name: "custom list cannot add unsupported images", accounts: []service.Account{textAccount}, changeGroup: func(g *service.Group) {
			g.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-image-2"}}
		}},
		{name: "custom list restricts defaults", accounts: []service.Account{defaultsAccount}, changeGroup: func(g *service.Group) {
			g.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-image-2"}}
		}, wantModels: []string{"gpt-image-2"}},
		{name: "unauthenticated", unauthorized: true, wantStatus: http.StatusUnauthorized},
		{name: "repository failure is not empty availability", repositoryErr: true, wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := group
			if tt.changeGroup != nil {
				tt.changeGroup(&g)
			}
			var repo service.AccountRepository = &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{1: tt.accounts}}
			if tt.repositoryErr {
				repo = imageStudioFailingAccountRepo{}
			}
			h := newGatewayModelsHandlerForTest(repo)
			h.apiKeyService = service.NewAPIKeyService(nil, imageStudioUserRepo{}, imageStudioGroupRepo{groups: []service.Group{g}}, imageStudioSubscriptionRepo{}, nil, nil, nil)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/groups/image-generation", nil)
			if !tt.unauthorized {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			}
			h.ImageGenerationGroups(c)
			status := tt.wantStatus
			if status == 0 {
				status = http.StatusOK
			}
			require.Equal(t, status, recorder.Code)
			if status != http.StatusOK {
				return
			}
			var result struct {
				Data []imageGenerationGroup `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			if len(tt.wantModels) == 0 {
				require.JSONEq(t, `[]`, string(mustJSONImageGroups(t, result.Data)))
				return
			}
			require.Len(t, result.Data, 1)
			require.Equal(t, g.ID, result.Data[0].ID)
			require.Equal(t, tt.wantModels, result.Data[0].ImageModels)
		})
	}
}

func mustJSONImageGroups(t *testing.T, groups []imageGenerationGroup) []byte {
	t.Helper()
	body, err := json.Marshal(groups)
	require.NoError(t, err)
	return body
}
