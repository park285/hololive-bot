package auth

import (
	"github.com/park285/shared-go/pkg/httputil"
)

type LoginRateLimiter = httputil.LoginFailureRateLimiter

func NewLoginRateLimiter() *LoginRateLimiter {
	return httputil.NewDefaultLoginFailureRateLimiter()
}
