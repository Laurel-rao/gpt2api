package middleware

import (
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/432539/gpt2api/pkg/logger"
)

const slowRequestThreshold = 5 * time.Second

var defaultSlowRequestStats = newSlowRequestStats()

type slowRequestKey struct {
	Method string
	Route  string
	Status int
}

type slowRequestStats struct {
	mu    sync.Mutex
	total uint64
	items map[slowRequestKey]uint64
}

// AccessLog 打印每一次 HTTP 访问的结构化日志。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		cost := time.Since(start)
		path := c.Request.URL.Path
		route := requestRoute(c)
		query := safeLogQuery(path, c.Request.URL.RawQuery)

		log := logger.L()
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("route", route),
			zap.String("query", query),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("cost", cost),
			zap.Int64("cost_ms", cost.Milliseconds()),
			zap.Int("response_bytes", c.Writer.Size()),
			zap.String("ip", c.ClientIP()),
			zap.String("ua", c.Request.UserAgent()),
			zap.String("request_id", getString(c, "request_id")),
		}
		if uid, ok := c.Get("user_id"); ok {
			fields = append(fields, zap.Any("user_id", uid))
		}
		if kid, ok := c.Get("key_id"); ok {
			fields = append(fields, zap.Any("key_id", kid))
		}
		if errs := c.Errors.ByType(gin.ErrorTypePrivate).String(); errs != "" {
			fields = append(fields, zap.String("err", errs))
		}

		status := c.Writer.Status()
		switch {
		case status >= 500:
			log.Error("http", fields...)
		case status >= 400:
			log.Warn("http", fields...)
		default:
			log.Info("http", fields...)
		}
		if cost >= slowRequestThreshold && !isAssetFetchPath(path) {
			total, routeTotal := defaultSlowRequestStats.Record(c.Request.Method, route, status)
			slowFields := append([]zap.Field{}, fields...)
			slowFields = append(slowFields,
				zap.Duration("threshold", slowRequestThreshold),
				zap.Uint64("slow_total", total),
				zap.Uint64("slow_route_total", routeTotal),
			)
			log.Warn("slow_http_request", slowFields...)
		}
	}
}

func newSlowRequestStats() *slowRequestStats {
	return &slowRequestStats{items: make(map[slowRequestKey]uint64)}
}

func (s *slowRequestStats) Record(method, route string, status int) (uint64, uint64) {
	if s == nil {
		return 0, 0
	}
	key := slowRequestKey{Method: method, Route: route, Status: status}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	s.items[key]++
	return s.total, s.items[key]
}

func requestRoute(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	if route := c.FullPath(); route != "" {
		return route
	}
	return c.Request.URL.Path
}

func isAssetFetchPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	for _, prefix := range []string{
		"/assets/",
		"/ecommerce-assets/",
		"/site-assets/",
		"/p/img/",
		"/p/vwf/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	switch {
	case path == "/favicon.ico":
		return true
	case strings.HasPrefix(path, "/apple-touch-icon"):
		return true
	default:
		return false
	}
}

func safeLogQuery(path, rawQuery string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(path)), "/p/vwf/") && rawQuery != "" {
		return "[redacted]"
	}
	return rawQuery
}

func getString(c *gin.Context, key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
