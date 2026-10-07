package middleware

import "github.com/gin-gonic/gin"

// TrustOnlyProxies tells the router which direct peers may vouch for a client
// address through X-Forwarded-For / X-Real-IP, which is what c.ClientIP()
// reads. By default Gin trusts every peer, so any caller could write whatever
// address it liked in that header, and everything built on the client address
// (the mesh-only routes, and node identity) could be faked from the internet.
//
// With only the local nginx trusted, headers count only when nginx sent the
// request; Gin then takes the address nginx itself appended on the right,
// not the one the caller put on the left. A node connecting straight over
// WireGuard is not a trusted proxy, so its address is the connection's.
//
// Do not pass an empty list: the Hub's own nginx connects from loopback,
// which is an allowed mesh network, so with no trusted proxy every request
// through nginx would look like it came from loopback.
func TrustOnlyProxies(r *gin.Engine, proxies []string) error {
	return r.SetTrustedProxies(proxies)
}
