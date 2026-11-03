package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const RouteTagKey = "route_tag"

func RouteTag(tag string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(RouteTagKey, tag)
		c.Next()
	}
}

func SetUpLogger(server *gin.Engine) {
	server.Use(redactTaskArtifactAccessQuery())
	server.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: common.LogWriter,
		Formatter: func(param gin.LogFormatterParams) string {
			var requestID string
			if param.Keys != nil {
				requestID, _ = param.Keys[common.RequestIdKey].(string)
			}
			tag, _ := param.Keys[RouteTagKey].(string)
			if tag == "" {
				tag = "web"
			}
			path := param.Path
			// OAuth callbacks carry one-time codes and state in the query string.
			// Redact the log value only; the handler still needs the original query.
			if strings.HasPrefix(path, "/api/oauth/") || strings.HasPrefix(path, "/oauth/") {
				path, _, _ = strings.Cut(path, "?")
			} else if param.Request.URL.RawQuery != "" {
				query := param.Request.URL.Query().Encode()
				path = param.Request.URL.EscapedPath() + relaycommon.SanitizeURLForLog("?"+query)
			} else {
				// param.Path keeps the query the request arrived with, including a
				// credential a handler took out of it, as the Gemini API routes do
				// with ?key=.
				path = param.Request.URL.Path
			}
			// Behind a reverse proxy the Host header may be rewritten to the backend
			// address; prefer the first X-Forwarded-Host entry so the log shows the
			// domain the client actually used.
			host := ""
			if param.Request != nil {
				host = param.Request.Host
				forwardedHost, _, _ := strings.Cut(param.Request.Header.Get("X-Forwarded-Host"), ",")
				if forwardedHost = strings.TrimSpace(forwardedHost); forwardedHost != "" {
					host = forwardedHost
				}
			}
			return fmt.Sprintf("[GIN] %s | %s | %s | %3d | %13v | %15s | %s | %7s %s\n",
				param.TimeStamp.Format("2006/01/02 - 15:04:05"),
				tag,
				requestID,
				param.StatusCode,
				param.Latency,
				param.ClientIP,
				host,
				param.Method,
				path,
			)
		},
	}))
}
