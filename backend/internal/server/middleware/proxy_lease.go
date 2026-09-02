package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ProxyLeaseContext 全局中间件：为每个请求安装账号多代理池的租约登记处。
//
// 账号绑定了多个代理时，调度会在选号后为本次请求挑一个出口代理并占用
// 「账号 × 代理」维度的并发槽位；这些槽位在请求结束时由此中间件统一释放。
// 账号没有多代理池时不会登记任何租约，中间件对既有链路是零开销的空操作。
func ProxyLeaseContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, release := service.ContextWithProxyLeases(c.Request.Context())
		defer release()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
