package httpapi

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

const adminDashboardCSP = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; frame-src 'none'; form-action 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; font-src 'self' data:; img-src 'self' data: https://*.ytimg.com https://*.ggpht.com; connect-src 'self'; media-src 'none'; worker-src 'self'; manifest-src 'self'"

func (r *API) recoverPanics() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recover() == nil {
				return
			}

			// request dump와 panic 값에는 credential이 섞일 수 있어 상관 ID만 남깁니다.
			r.logger.Error("admin HTTP request panicked", slog.String("request_id", httpx.RequestID(c)))

			if c.Writer.Written() {
				c.Abort()

				return
			}

			httpx.Abort(c, contract.NewError(http.StatusInternalServerError, "Internal server error"))
		}()

		c.Next()
	}
}

func (r *API) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("X-XSS-Protection", "1; mode=block")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Content-Security-Policy", adminDashboardCSP)
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")

		if isAdminAPI(c.Request.URL.Path) || c.Request.URL.Path == "/admin/meta.json" {
			header.Set("Cache-Control", "no-store, private")
			header.Set("Pragma", "no-cache")
			header.Set("Expires", "0")
		}

		if r.cfg.Security.ForceHTTPS {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}

const etagMaxBufferBytes = 64 << 10

type etagWriter struct {
	gin.ResponseWriter

	body     strings.Builder
	status   int
	limit    int
	overflow bool
}

func (w *etagWriter) WriteHeader(status int) {
	if w.overflow {
		w.ResponseWriter.WriteHeader(status)

		return
	}

	w.status = status
}

func (w *etagWriter) WriteHeaderNow() {}

func (w *etagWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	if !w.overflow && w.body.Len()+len(data) > w.limit {
		w.startOverflow()
	}

	if w.overflow {
		out, err := w.ResponseWriter.Write(data)
		if err != nil {
			return out, fmt.Errorf("write: %w", err)
		}

		return out, nil
	}

	out, err := w.body.Write(data)
	if err != nil {
		return out, fmt.Errorf("write: %w", err)
	}

	return out, nil
}

func (w *etagWriter) WriteString(s string) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	if !w.overflow && w.body.Len()+len(s) > w.limit {
		w.startOverflow()
	}

	if w.overflow {
		out, err := w.ResponseWriter.WriteString(s)
		if err != nil {
			return out, fmt.Errorf("write string: %w", err)
		}

		return out, nil
	}

	out, err := w.body.WriteString(s)
	if err != nil {
		return out, fmt.Errorf("write string: %w", err)
	}

	return out, nil
}

func (w *etagWriter) startOverflow() {
	w.overflow = true
	w.ResponseWriter.WriteHeader(w.status)
	writeBufferedString(w.ResponseWriter, w.body.String())
	w.body.Reset()
}

func (w *etagWriter) Status() int {
	if w.status != 0 {
		return w.status
	}

	return w.ResponseWriter.Status()
}

func (w *etagWriter) Size() int {
	if w.overflow {
		return w.ResponseWriter.Size()
	}

	return w.body.Len()
}

func (w *etagWriter) Written() bool {
	return w.status != 0
}

func (w *etagWriter) flush(req *http.Request) {
	if w.overflow {
		return
	}

	if w.status == 0 {
		w.status = http.StatusOK
	}

	if w.status != http.StatusOK {
		w.ResponseWriter.WriteHeader(w.status)
		writeBufferedString(w.ResponseWriter, w.body.String())

		return
	}

	h := fnv.New64a()
	writeHash(h, w.body.String())

	etag := `"` + strconv.FormatUint(h.Sum64(), 16) + `"`
	w.ResponseWriter.Header().Set("ETag", etag)

	if etagMatches(req.Header.Get("If-None-Match"), etag) {
		w.ResponseWriter.WriteHeader(http.StatusNotModified)

		return
	}

	w.ResponseWriter.WriteHeader(http.StatusOK)
	writeBufferedString(w.ResponseWriter, w.body.String())
}

func writeBufferedString(w gin.ResponseWriter, body string) {
	if _, err := w.WriteString(body); err != nil {
		return
	}
}

func writeHash(h interface{ Write([]byte) (int, error) }, body string) {
	if _, err := h.Write([]byte(body)); err != nil {
		return
	}
}

func (r *API) etag() gin.HandlerFunc {
	return func(c *gin.Context) {
		if etagSkipped(c.Request) {
			c.Next()

			return
		}

		writer := &etagWriter{ResponseWriter: c.Writer, limit: etagMaxBufferBytes}

		c.Writer = writer
		c.Next()
		writer.flush(c.Request)
	}
}

func etagSkipped(req *http.Request) bool {
	return req.Method != http.MethodGet || req.URL.Path == "/admin/meta.json" || isAdminAPI(req.URL.Path) || req.Header.Get("Upgrade") != ""
}

func etagMatches(header, etag string) bool {
	return header == etag || header == strings.Trim(etag, `"`)
}
