package middleware

import (
	"github.com/gin-gonic/gin"
)

func VPNOnly(allowedNetworks []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		c.Set("vpn_ip", clientIP)

		c.Next()
	}
}
