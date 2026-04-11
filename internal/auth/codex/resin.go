package codex

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/proxyutil"
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

func ResolveResinHTTPRoute(cfg *config.Config, auth *cliproxyauth.Auth, rawTarget string) (*ResinRoute, error) {
	return ResolveResinHTTPRouteForAccount(cfg, StableResinAccount(auth), rawTarget)
}

func ResolveResinHTTPRouteForAccount(cfg *config.Config, account string, rawTarget string) (*ResinRoute, error) {
	return resolveResinRoute(cfg, account, rawTarget, false)
}

func ResolveResinWebsocketRoute(cfg *config.Config, auth *cliproxyauth.Auth, rawTarget string) (*ResinRoute, error) {
	return ResolveResinWebsocketRouteForAccount(cfg, StableResinAccount(auth), rawTarget)
}

func ResolveResinWebsocketRouteForAccount(cfg *config.Config, account string, rawTarget string) (*ResinRoute, error) {
	return resolveResinRoute(cfg, account, rawTarget, true)
}

func ApplyResinReverseProxy(req *http.Request, cfg *config.Config, auth *cliproxyauth.Auth) (bool, error) {
	return ApplyResinReverseProxyForAccount(req, cfg, StableResinAccount(auth))
}

func ApplyResinReverseProxyForAccount(req *http.Request, cfg *config.Config, account string) (bool, error) {
	if req == nil || req.URL == nil {
		return false, nil
	}
	route, err := ResolveResinHTTPRouteForAccount(cfg, account, req.URL.String())
	if err != nil {
		return false, err
	}
	if route == nil {
		return false, nil
	}
	parsedURL, errParse := url.Parse(route.URL)
	if errParse != nil {
		return false, fmt.Errorf("parse Resin route URL failed: %w", errParse)
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.URL = parsedURL
	req.Host = ""
	req.Header.Set(ResinAccountHeader, route.Account)
	return true, nil
}

func NewDirectHTTPClient() *http.Client {
	return &http.Client{Transport: proxyutil.NewDirectTransport()}
}

func resinConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.ResinURL) != "" && strings.TrimSpace(cfg.ResinPlatformName) != ""
}

func resolveResinRoute(cfg *config.Config, account string, rawTarget string, websocket bool) (*ResinRoute, error) {
	if !resinConfigured(cfg) {
		return nil, nil
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return nil, nil
	}

	resinBase, errParseBase := url.Parse(strings.TrimSpace(cfg.ResinURL))
	if errParseBase != nil {
		return nil, fmt.Errorf("parse Resin URL failed: %w", errParseBase)
	}
	if resinBase.Scheme == "" || resinBase.Host == "" {
		return nil, fmt.Errorf("Resin URL missing scheme/host")
	}

	target, errParseTarget := url.Parse(strings.TrimSpace(rawTarget))
	if errParseTarget != nil {
		return nil, fmt.Errorf("parse target URL failed: %w", errParseTarget)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("target URL missing scheme/host")
	}

	clientScheme, targetProtocol, errScheme := resinTargetProtocol(target.Scheme, websocket)
	if errScheme != nil {
		return nil, errScheme
	}

	return &ResinRoute{
		URL: (&url.URL{
			Scheme:   clientScheme,
			User:     resinBase.User,
			Host:     resinBase.Host,
			Path:     buildResinRoutePath(resinBase.Path, cfg.ResinPlatformName, targetProtocol, target.Host, target.Path),
			RawQuery: target.RawQuery,
		}).String(),
		Account: account,
	}, nil
}

func resinTargetProtocol(targetScheme string, websocket bool) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(targetScheme)) {
	case "http":
		if websocket {
			return "ws", "http", nil
		}
		return "http", "http", nil
	case "https":
		if websocket {
			return "ws", "https", nil
		}
		return "https", "https", nil
	case "ws":
		if websocket {
			return "ws", "http", nil
		}
	case "wss":
		if websocket {
			return "ws", "https", nil
		}
	}
	if websocket {
		return "", "", fmt.Errorf("unsupported websocket target scheme: %s", targetScheme)
	}
	return "", "", fmt.Errorf("unsupported HTTP target scheme: %s", targetScheme)
}

func buildResinRoutePath(basePath string, platform string, protocol string, host string, targetPath string) string {
	segments := make([]string, 0, 5)
	if trimmed := strings.Trim(strings.TrimSpace(basePath), "/"); trimmed != "" {
		segments = append(segments, trimmed)
	}
	segments = append(segments,
		strings.Trim(strings.TrimSpace(platform), "/"),
		strings.TrimSpace(protocol),
		strings.TrimSpace(host),
	)
	if trimmed := strings.TrimPrefix(targetPath, "/"); trimmed != "" {
		segments = append(segments, trimmed)
	}
	return "/" + strings.Join(segments, "/")
}
