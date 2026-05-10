package codex

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const ResinAccountHeader = "X-Resin-Account"

type ResinRoute struct {
	URL     string
	Account string
}

func StableResinAccount(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if fileName := strings.TrimSpace(auth.FileName); fileName != "" {
		return fileName
	}
	return strings.TrimSpace(auth.ID)
}

func ResolveResinHTTPRoute(cfg *config.Config, target string, auth *cliproxyauth.Auth) (ResinRoute, bool, error) {
	return ResolveResinHTTPRouteForAccount(cfg, target, StableResinAccount(auth))
}

func ResolveResinHTTPRouteForAccount(cfg *config.Config, target, account string) (ResinRoute, bool, error) {
	if _, _, ok := resinConfigured(cfg); !ok || strings.TrimSpace(account) == "" {
		return ResinRoute{}, false, nil
	}
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return ResinRoute{}, false, err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return ResinRoute{}, false, nil
	}
	return resolveResinRoute(cfg, target, account, false)
}

func ResolveResinWebsocketRoute(cfg *config.Config, target string, auth *cliproxyauth.Auth) (ResinRoute, bool, error) {
	return ResolveResinWebsocketRouteForAccount(cfg, target, StableResinAccount(auth))
}

func ResolveResinWebsocketRouteForAccount(cfg *config.Config, target, account string) (ResinRoute, bool, error) {
	return resolveResinRoute(cfg, target, account, true)
}

func ApplyResinReverseProxy(cfg *config.Config, req *http.Request, auth *cliproxyauth.Auth) bool {
	return ApplyResinReverseProxyForAccount(cfg, req, StableResinAccount(auth))
}

func ApplyResinReverseProxyForAccount(cfg *config.Config, req *http.Request, account string) bool {
	if req == nil || req.URL == nil {
		return false
	}
	route, ok, err := ResolveResinHTTPRouteForAccount(cfg, req.URL.String(), account)
	if err != nil || !ok {
		return false
	}
	resinURL, err := url.Parse(route.URL)
	if err != nil {
		return false
	}
	req.URL = resinURL
	req.Host = ""
	req.RequestURI = ""
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set(ResinAccountHeader, route.Account)
	return true
}

func NewDirectHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Transport: newDirectHTTPTransport()}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client
}

func resinConfigured(cfg *config.Config) (string, string, bool) {
	if cfg == nil {
		return "", "", false
	}
	resinURL := strings.TrimSpace(cfg.ResinURL)
	platform := strings.Trim(strings.TrimSpace(cfg.ResinPlatformName), "/")
	if resinURL == "" || platform == "" {
		return "", "", false
	}
	return resinURL, platform, true
}

func resolveResinRoute(cfg *config.Config, target, account string, websocket bool) (ResinRoute, bool, error) {
	resinBase, platform, ok := resinConfigured(cfg)
	account = strings.TrimSpace(account)
	if !ok || account == "" {
		return ResinRoute{}, false, nil
	}

	targetURL, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return ResinRoute{}, false, err
	}
	if strings.TrimSpace(targetURL.Host) == "" {
		return ResinRoute{}, false, fmt.Errorf("resin target host is empty")
	}
	targetScheme := strings.ToLower(targetURL.Scheme)
	if !websocket && targetScheme != "http" && targetScheme != "https" {
		return ResinRoute{}, false, nil
	}
	targetProtocol, ok := resinTargetProtocol(targetScheme)
	if !ok {
		return ResinRoute{}, false, nil
	}

	resinURL, err := url.Parse(resinBase)
	if err != nil {
		return ResinRoute{}, false, err
	}
	if strings.TrimSpace(resinURL.Scheme) == "" || strings.TrimSpace(resinURL.Host) == "" {
		return ResinRoute{}, false, fmt.Errorf("resin URL must include scheme and host")
	}

	routeURL := *resinURL
	if websocket {
		routeURL.Scheme = "ws"
	}
	routeURL.RawQuery = targetURL.RawQuery
	routeURL.ForceQuery = targetURL.ForceQuery
	routeURL.Fragment = ""
	routeURL.RawFragment = ""
	routePath, rawRoutePath := buildResinRoutePath(routeURL.EscapedPath(), platform, targetProtocol, targetURL)
	routeURL.Path = routePath
	routeURL.RawPath = rawRoutePath

	return ResinRoute{URL: routeURL.String(), Account: account}, true, nil
}

func resinTargetProtocol(scheme string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "ws":
		return "http", true
	case "https", "wss":
		return "https", true
	default:
		return "", false
	}
}

func buildResinRoutePath(baseEscapedPath, platform, targetProtocol string, target *url.URL) (string, string) {
	segments := make([]string, 0, 5)
	if basePath := strings.Trim(baseEscapedPath, "/"); basePath != "" {
		segments = append(segments, basePath)
	}
	segments = append(segments, url.PathEscape(platform), targetProtocol, target.Host)

	targetEscapedPath := ""
	if target != nil {
		targetEscapedPath = target.EscapedPath()
	}
	if trimmedTargetPath := strings.TrimPrefix(targetEscapedPath, "/"); trimmedTargetPath != "" {
		segments = append(segments, trimmedTargetPath)
	}

	rawPath := "/" + strings.Join(segments, "/")
	if targetEscapedPath == "/" && !strings.HasSuffix(rawPath, "/") {
		rawPath += "/"
	}
	pathValue, err := url.PathUnescape(rawPath)
	if err != nil {
		return rawPath, ""
	}
	return pathValue, rawPath
}

func newDirectHTTPTransport() http.RoundTripper {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || transport == nil {
		return &http.Transport{Proxy: nil}
	}
	clone := transport.Clone()
	clone.Proxy = nil
	return clone
}
