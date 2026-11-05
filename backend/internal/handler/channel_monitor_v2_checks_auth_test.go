package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupCheckTestSubscriptions struct{ err error }

func (s groupCheckTestSubscriptions) GetEligibleSubscription(context.Context, int64, *service.Group) (*service.UserSubscription, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &service.UserSubscription{}, nil
}
func TestGroupCheckInternalAuthValidatesIdentityAndSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := int64(4)
	key := &service.APIKey{ID: 7, UserID: 3, User: &service.User{ID: 3}, GroupID: &id, Group: &service.Group{ID: id, SubscriptionType: service.SubscriptionTypeSubscription}}
	for _, tc := range []struct {
		name          string
		expected      *service.APIKey
		subscriptions groupCheckTestSubscriptions
		status        int
	}{
		{"eligible", key, groupCheckTestSubscriptions{}, http.StatusNoContent},
		{"wrong identity", &service.APIKey{ID: 8, UserID: 3, GroupID: &id}, groupCheckTestSubscriptions{}, http.StatusForbidden},
		{"ineligible subscription", key, groupCheckTestSubscriptions{errors.New("expired")}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/", groupCheckInternalAuth(probeTestKeys{key}, tc.subscriptions)(tc.expected), func(c *gin.Context) {
				_, ok := middleware.GetSubscriptionFromContext(c)
				require.True(t, ok)
				got, ok := middleware.GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.Equal(t, key.ID, got.ID)
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, tc.status, w.Code)
		})
	}
}
