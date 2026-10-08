//go:build unit

package routes

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mixedGroupImagesAccounts struct {
	compatibleImagesAccounts
	byGroup map[int64][]service.Account
}

func (r mixedGroupImagesAccounts) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]service.Account, error) {
	return r.byGroup[groupID], nil
}

func TestGatewayRoutesMultiGroupImagesResolveSelectedCompositeGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, prefix := range []string{"/v1", ""} {
		for _, scenario := range []string{"generation", "json_edit", "multipart_edit", "composite_alias"} {
			t.Run(prefix+"/"+scenario, func(t *testing.T) {
				const imageModel = "gpt-image-2"
				publicModel := imageModel
				first := &service.Group{ID: 10, Platform: service.PlatformOpenAI, Status: service.StatusActive}
				price := 0.17
				images := &service.Group{ID: 20, Platform: service.PlatformComposite, Status: service.StatusActive,
					AllowImageGeneration: true, RateMultiplier: 1, ImagePrice1K: &price, ImagePrice2K: &price, ImagePrice4K: &price}
				var routes []service.CompositeModelRoute
				if scenario == "composite_alias" {
					publicModel = "public-image"
					first.Platform = service.PlatformComposite
					// The first group's alias must not rewrite the model before the
					// supported-group filter selects the image group.
					for _, group := range []*service.Group{first, images} {
						upstreamModel := imageModel
						if group == first {
							upstreamModel = "gpt-5"
						}
						routes = append(routes, service.CompositeModelRoute{ID: group.ID, GroupID: group.ID,
							PublicModel: publicModel, MatchType: service.CompositeRouteMatchExact,
							TargetPlatform: service.PlatformOpenAI, UpstreamModel: upstreamModel,
							Endpoint: service.CompositeRouteEndpointImages, Enabled: true})
					}
				}
				account := service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://images.example/v1",
						"model_mapping": map[string]any{publicModel: imageModel, imageModel: imageModel}}}
				repo := mixedGroupImagesAccounts{
					compatibleImagesAccounts: compatibleImagesAccounts{accounts: []service.Account{account}},
					byGroup: map[int64][]service.Account{
						first.ID: {{ID: 8, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
							Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5": "gpt-5"}}}},
						images.ID: {account},
					},
				}
				cfg := &config.Config{RunMode: config.RunModeSimple,
					Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024, TextMaxBodySize: 1024 * 1024}}
				upstream, usage := &compatibleImagesUpstream{}, &compatibleImagesUsage{}
				billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billing.Stop)
				openAI := service.NewOpenAIGatewayService(repo, usage, nil, nil, nil, nil, nil, cfg,
					nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
				gateway := service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, cfg,
					nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				h := &handler.Handlers{
					Gateway: handler.NewGatewayHandler(gateway, openAI, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil),
					OpenAIGateway: handler.NewOpenAIGatewayHandler(openAI, service.NewConcurrencyService(nil), nil, billing,
						service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg),
					AsyncImage: handler.NewAsyncImageHandler(nil, nil, nil),
				}
				key := &service.APIKey{ID: 202, GroupID: &first.ID, Group: first, User: &service.User{ID: 303},
					GroupRoutes: []service.APIKeyGroupRoute{
						{GroupID: first.ID, Priority: 0, Enabled: true, Group: first},
						{GroupID: images.ID, Priority: 1, Enabled: true, Group: images},
					}}
				var state *servermiddleware.APIKeyRouteState
				router := gin.New()
				RegisterGatewayRoutes(router, h, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
					plan, err := service.NewAPIKeyRouteCoordinator().BuildPlan(key, nil)
					require.NoError(t, err)
					initial, ok := plan.APIKeyForCandidate(0)
					require.True(t, ok)
					state = &servermiddleware.APIKeyRouteState{Plan: plan, InitialGroupID: first.ID}
					c.Set(string(servermiddleware.ContextKeyAPIKey), initial)
					c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 303})
					servermiddleware.SetAPIKeyRouteState(c, state)
					c.Next()
				}), nil, nil, nil, nil, service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: routes}), cfg)

				path, contentType := prefix+"/images/generations", "application/json"
				body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"a red five-pointed star on a white background","n":1}`, publicModel))
				if scenario == "json_edit" {
					path = prefix + "/images/edits"
					body = []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw","images":[{"image_url":"https://source.example/input.png"}]}`, publicModel))
				}
				if scenario == "multipart_edit" {
					path = prefix + "/images/edits"
					var buf bytes.Buffer
					writer := multipart.NewWriter(&buf)
					require.NoError(t, writer.WriteField("model", publicModel))
					require.NoError(t, writer.WriteField("prompt", "draw"))
					part, err := writer.CreateFormFile("image", "input.png")
					require.NoError(t, err)
					_, err = part.Write([]byte("fixture-image"))
					require.NoError(t, err)
					require.NoError(t, writer.Close())
					body, contentType = buf.Bytes(), writer.FormDataContentType()
				}
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				req.Header.Set("Content-Type", contentType)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Equal(t, images.ID, state.InitialGroupID)
				require.Zero(t, state.SwitchCount, "model filtering is not an upstream failover")
				require.Equal(t, first.ID, *key.GroupID, "shared API key must remain unchanged")
				require.Equal(t, []int64{account.ID}, upstream.accountIDs)
				require.Contains(t, string(upstream.body), imageModel)
				require.Contains(t, rec.Body.String(), "aW1hZ2U=")
				require.Len(t, usage.logs, 1)
				require.Equal(t, &images.ID, usage.logs[0].GroupID)
				require.Equal(t, publicModel, usage.logs[0].RequestedModel)
			})
		}
	}
}
