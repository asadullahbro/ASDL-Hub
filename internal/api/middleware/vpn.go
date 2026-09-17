package middleware

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

// VPNOnly restricts a route to clients whose IP falls inside one of the
// given CIDR ranges (the WireGuard mesh, loopback, etc). Malformed entries
// in allowedNetworks are skipped rather than treated as a match-all.
func VPNOnly(allowedNetworks []string) gin.HandlerFunc {
	nets := make([]*net.IPNet, 0, len(allowedNetworks))
	for _, cidr := range allowedNetworks {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		nets = append(nets, ipNet)
	}

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "unable to determine client ip"})
			c.Abort()
			return
		}

		allowed := false
		for _, ipNet := range nets {
			if ipNet.Contains(clientIP) {
				allowed = true
				break
			}
		}

		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "access restricted to the mesh network"})
			c.Abort()
			return
		}

		c.Set("vpn_ip", clientIP.String())
		c.Next()
	}
}
