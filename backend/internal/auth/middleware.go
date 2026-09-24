package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const claimsKey = "cicd.auth.claims"

func Middleware(manager *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		rawToken := ""
		if strings.HasPrefix(header, "Bearer ") {
			rawToken = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		} else if isWebSocketUpgrade(c) {
			// Browsers cannot set Authorization on a WebSocket constructor. The
			// terminal client sends a bearer token as a secondary subprotocol;
			// unlike a query parameter it is not written into access URLs/logs.
			rawToken = websocketBearerToken(c.GetHeader("Sec-WebSocket-Protocol"))
		}
		if rawToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "unauthorized", "message": "请先登录"}})
			return
		}
		claims, err := manager.Parse(rawToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "unauthorized", "message": "登录已过期，请重新登录"}})
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}

func isWebSocketUpgrade(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("Upgrade")), "websocket")
}

func websocketBearerToken(header string) string {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "bearer.") {
			return strings.TrimSpace(strings.TrimPrefix(value, "bearer."))
		}
	}
	return ""
}

func ClaimsFrom(c *gin.Context) (Claims, bool) {
	value, ok := c.Get(claimsKey)
	if !ok {
		return Claims{}, false
	}
	claims, ok := value.(Claims)
	return claims, ok
}
