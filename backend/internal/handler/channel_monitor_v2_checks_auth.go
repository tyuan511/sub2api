package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type groupCheckIdentityStore interface {
	GetOrCreate(context.Context, int64, int64) (int64, error)
}
type groupCheckUserLoader interface {
	GetByID(context.Context, int64) (*service.User, error)
}
type groupCheckKeyProvider struct {
	identities groupCheckIdentityStore
	users      groupCheckUserLoader
	groups     channelMonitorV2GroupAuthorizer
}

func (p *groupCheckKeyProvider) GroupCheckInternalKey(ctx context.Context, userID, groupID int64) (*service.APIKey, error) {
	if p == nil || p.identities == nil || p.users == nil || p.groups == nil {
		return nil, errors.New("internal identity dependencies unavailable")
	}
	user, err := p.users.GetByID(ctx, userID)
	if err != nil || user == nil || !user.IsActive() {
		return nil, errors.New("user unavailable")
	}
	// 管理员：可检测任意活跃分组（含专属/订阅分组，订阅资格仍由网关计费校验把关）。
	if user.Role == service.RoleAdmin {
		group, err := p.groups.GetGroupByID(ctx, groupID)
		if err != nil || group == nil || !group.IsActive() {
			return nil, errors.New("group unavailable")
		}
		return p.buildInternalKey(ctx, user, group, userID, groupID)
	}
	// 普通用户：仅限自己可用的分组。
	groups, err := p.groups.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		group := &groups[i]
		if group.ID != groupID || !group.IsActive() {
			continue
		}
		return p.buildInternalKey(ctx, user, group, userID, groupID)
	}
	return nil, errors.New("group unavailable")
}

func (p *groupCheckKeyProvider) buildInternalKey(ctx context.Context, user *service.User, group *service.Group, userID, groupID int64) (*service.APIKey, error) {
	id, err := p.identities.GetOrCreate(ctx, userID, groupID)
	if err != nil || id <= 0 {
		return nil, errors.New("internal identity unavailable")
	}
	return &service.APIKey{ID: id, UserID: userID, GroupID: &groupID, Group: group, User: user, Status: service.StatusAPIKeyDisabled, Name: "分组检测"}, nil
}

type groupCheckSubscriptions interface {
	GetEligibleSubscription(context.Context, int64, *service.Group) (*service.UserSubscription, error)
}

// Internal-only middleware: reloads the identity and its authorization immediately
// before dispatch. Gateway handlers perform the normal billing eligibility check.
func groupCheckInternalAuth(provider groupCheckInternalKeyProvider, subscriptions groupCheckSubscriptions) func(*service.APIKey) gin.HandlerFunc {
	return func(expected *service.APIKey) gin.HandlerFunc {
		return func(c *gin.Context) {
			if provider == nil || expected == nil || expected.GroupID == nil {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			key, err := provider.GroupCheckInternalKey(c.Request.Context(), expected.UserID, *expected.GroupID)
			if err != nil || key == nil || key.ID != expected.ID || key.User == nil || key.Group == nil || key.GroupID == nil || *key.GroupID != *expected.GroupID || key.UserID != expected.UserID {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			if key.Group.IsSubscriptionType() {
				if subscriptions == nil {
					c.AbortWithStatus(http.StatusServiceUnavailable)
					return
				}
				sub, err := subscriptions.GetEligibleSubscription(c.Request.Context(), key.UserID, key.Group)
				if err != nil || sub == nil {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
				c.Set(string(middleware.ContextKeySubscription), sub)
			}
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: key.User.Concurrency})
			c.Set(string(middleware.ContextKeyUserRole), key.User.Role)
			ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, key.UserID)
			ctx = context.WithValue(ctx, ctxkey.Group, key.Group)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		}
	}
}
