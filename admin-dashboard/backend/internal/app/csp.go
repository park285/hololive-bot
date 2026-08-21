package app

import "github.com/gin-gonic/gin"

const adminDashboardCSP = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; frame-src 'none'; form-action 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; font-src 'self' data:; img-src 'self' data: https://*.ytimg.com https://*.ggpht.com; connect-src 'self'; media-src 'none'; worker-src 'self'; manifest-src 'self'"

// hardenedCSP runs after the legacy/common security-header middleware so it can
// narrow the policy without coupling CSP evolution to the rest of middleware.go.
func (r *Runtime) hardenedCSP() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", adminDashboardCSP)
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		c.Next()
	}
}
