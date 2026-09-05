package admin

import (
	"bytes"
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type rolloutSettingsHandlerRepo struct {
	service.SettingRepository
	value string
}

func (r *rolloutSettingsHandlerRepo) GetValue(context.Context, string) (string, error) {
	return r.value, nil
}
func (r *rolloutSettingsHandlerRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}

type rolloutSettingsUserRepo struct{ service.UserRepository }

func (r *rolloutSettingsUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	if id == 5 || id == 9 {
		return &service.User{ID: id}, nil
	}
	return nil, service.ErrUserNotFound
}
func (r *rolloutSettingsUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func TestAPIKeyRoutingRolloutHandlerValidationAndSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		body     string
		status   int
		expected string
	}{
		{`{"user_ids":[9,5,9]}`, 200, `{"user_ids":[5,9]}`},
		{`{"user_ids":[]}`, 200, `{"user_ids":[]}`},
		{`{}`, 400, ""}, {`{"user_ids":null}`, 400, ""},
		{`{"user_ids":[0]}`, 400, ""}, {`{"user_ids":[1.5]}`, 400, ""},
		{`{"user_ids":["5"]}`, 400, ""}, {`{"user_ids":[999]}`, 400, ""},
	} {
		t.Run(test.body, func(t *testing.T) {
			repo := &rolloutSettingsHandlerRepo{}
			h := NewSettingHandler(service.NewSettingService(repo, nil), nil, nil, nil, nil, nil, nil)
			h.SetStepUpDeps(nil, service.NewUserService(&rolloutSettingsUserRepo{}, nil, nil, nil))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("PUT", "/api/v1/admin/settings/api-key-routing-rollout", bytes.NewBufferString(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			h.UpdateAPIKeyRoutingRollout(c)
			require.Equal(t, test.status, rec.Code, rec.Body.String())
			require.Equal(t, test.expected, repo.value)
		})
	}
}
