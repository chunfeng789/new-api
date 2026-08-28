package router

import (
	"bytes"
	"embed"
	"net"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS                          embed.FS
	IndexPage                        []byte
	CloudflareWebAnalyticsToken      string
	CloudflareWebAnalyticsHostTokens map[string]string
}

func SetWebRouter(router *gin.Engine, assets WebAssets) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", frontendFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		analyticsToken := cloudflareWebAnalyticsTokenForHost(
			c.Request.Host,
			assets.CloudflareWebAnalyticsToken,
			assets.CloudflareWebAnalyticsHostTokens,
		)
		c.Data(
			http.StatusOK,
			"text/html; charset=utf-8",
			injectCloudflareWebAnalytics(assets.IndexPage, analyticsToken),
		)
	})
}

func cloudflareWebAnalyticsTokenForHost(requestHost string, defaultToken string, hostTokens map[string]string) string {
	if len(hostTokens) == 0 {
		return defaultToken
	}

	host := requestHost
	if parsedHost, _, err := net.SplitHostPort(requestHost); err == nil {
		host = parsedHost
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")

	matchedHostLength := -1
	matchedToken := ""
	for configuredHost, token := range hostTokens {
		if host != configuredHost && !strings.HasSuffix(host, "."+configuredHost) {
			continue
		}
		if len(configuredHost) > matchedHostLength {
			matchedHostLength = len(configuredHost)
			matchedToken = token
		}
	}
	return matchedToken
}

func injectCloudflareWebAnalytics(indexPage []byte, token string) []byte {
	analyticsInjectBuilder := &strings.Builder{}
	if token != "" {
		analyticsInjectBuilder.WriteString("<!-- Cloudflare Web Analytics -->\n")
		analyticsInjectBuilder.WriteString("<script type=\"module\" src=\"https://static.cloudflareinsights.com/beacon.min.js\" data-cf-beacon='{\"token\":\"")
		analyticsInjectBuilder.WriteString(token)
		analyticsInjectBuilder.WriteString("\"}'></script>\n")
		analyticsInjectBuilder.WriteString("<!-- End Cloudflare Web Analytics -->\n")
	}
	analyticsInjectBuilder.WriteString("<!--Cloudflare Web Analytics QuantumNous-->\n")
	return bytes.ReplaceAll(
		indexPage,
		[]byte("<!--Cloudflare Web Analytics-->\n"),
		[]byte(analyticsInjectBuilder.String()),
	)
}
