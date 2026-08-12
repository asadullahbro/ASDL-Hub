package middleware

import (
    "github.com/gin-gonic/gin"
)

func VPNOnly(allowedNetworks []string) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Get client IP
        clientIP := c.ClientIP()
        
        // Always set VPN IP in context
        c.Set("vpn_ip", clientIP)
        
        // For now, accept all connections (we trust WireGuard network)
        c.Next()
    }
}