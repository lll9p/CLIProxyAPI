package resin

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

const (
	accountHeader        = "X-Resin-Account"
	maxAccountHeaderSize = 8 << 10
)

var directTransport http.RoundTripper = proxyutil.NewDirectTransport()

type routePlan struct {
	resinScheme      string
	resinHost        string
	resinEscapedPath string
	platform         string
	platformSegment  string
	account          string
}

type routingTransport struct {
	plan     routePlan
	fallback http.RoundTripper
}

// WrapTransport routes eligible account-provider HTTP requests through Resin.
func WrapTransport(cfg *config.Config, auth *cliproxyauth.Auth, fallback http.RoundTripper) http.RoundTripper {
	plan, ok := newRoutePlan(cfg, auth)
	if !ok {
		return fallback
	}

	if wrapped, okWrapped := fallback.(*routingTransport); okWrapped {
		if wrapped.plan == plan {
			return wrapped
		}
		fallback = unwrapTransport(wrapped)
	}

	return &routingTransport{plan: plan, fallback: fallback}
}

// PrepareWebSocket prepares an eligible WebSocket dial through Resin and returns
// a stable opaque key suitable for connection reuse decisions.
func PrepareWebSocket(
	cfg *config.Config,
	auth *cliproxyauth.Auth,
	target *url.URL,
	headers http.Header,
) (dialURL *url.URL, dialHeaders http.Header, dialKey string, routed bool) {
	plan, planOK := newRoutePlan(cfg, auth)
	if planOK {
		if routedURL, routeOK := routeURL(plan, target, true); routeOK {
			routedHeaders := headers.Clone()
			if routedHeaders == nil {
				routedHeaders = make(http.Header)
			}
			deleteHeaderCaseInsensitive(routedHeaders, "Host")
			deleteHeaderCaseInsensitive(routedHeaders, accountHeader)
			routedHeaders.Set(accountHeader, plan.account)
			return routedURL, routedHeaders, websocketDialKey(routedURL, routedHeaders, cfg, auth, plan, true), true
		}
	}

	return target, headers, websocketDialKey(target, headers, cfg, auth, routePlan{}, false), false
}

func newRoutePlan(cfg *config.Config, auth *cliproxyauth.Auth) (routePlan, bool) {
	if cfg == nil || !supportsAuth(auth) {
		return routePlan{}, false
	}

	account := stableAccount(auth)
	if !validAccount(account) {
		return routePlan{}, false
	}

	platform := strings.TrimSpace(cfg.ResinPlatformName)
	if !validPlatform(platform) {
		return routePlan{}, false
	}

	rawBase := strings.TrimSpace(cfg.ResinURL)
	if rawBase == "" || strings.Contains(rawBase, "#") {
		return routePlan{}, false
	}
	base, errParse := url.Parse(rawBase)
	if errParse != nil || base.Opaque != "" || base.User != nil || base.Host == "" || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" {
		return routePlan{}, false
	}

	scheme := strings.ToLower(base.Scheme)
	if scheme != "http" && scheme != "https" {
		return routePlan{}, false
	}

	return routePlan{
		resinScheme:      scheme,
		resinHost:        base.Host,
		resinEscapedPath: exactEscapedPath(base),
		platform:         platform,
		platformSegment:  url.PathEscape(platform),
		account:          account,
	}, true
}

func stableAccount(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return ""
	}
	if account := strings.TrimSpace(auth.FileName); account != "" {
		return account
	}
	return strings.TrimSpace(auth.ID)
}

func supportsAuth(auth *cliproxyauth.Auth) bool {
	if auth == nil || auth.AuthKind() == cliproxyauth.AuthKindAPIKey {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(auth.Provider)) {
	case "codex", "claude", "antigravity", "gemini", "gemini-interactions", "kimi", "xai", "vertex":
		return true
	default:
		return false
	}
}

func validAccount(account string) bool {
	if account == "" || len(account) > maxAccountHeaderSize || !utf8.ValidString(account) {
		return false
	}
	for _, value := range account {
		if unicode.IsControl(value) {
			return false
		}
	}
	return true
}

func validPlatform(platform string) bool {
	if platform == "" {
		return false
	}
	for _, value := range platform {
		if value == '/' || value == '.' || value == ':' || unicode.IsControl(value) {
			return false
		}
	}
	return true
}

func routeURL(plan routePlan, target *url.URL, websocket bool) (*url.URL, bool) {
	if target == nil || target.Opaque != "" || target.Host == "" {
		return nil, false
	}

	targetScheme := strings.ToLower(target.Scheme)
	protocol := targetScheme
	resinScheme := plan.resinScheme
	if websocket {
		if plan.resinScheme != "http" {
			return nil, false
		}
		resinScheme = "ws"
		switch targetScheme {
		case "ws":
			protocol = "http"
		case "wss":
			protocol = "https"
		default:
			return nil, false
		}
	} else if targetScheme != "http" && targetScheme != "https" {
		return nil, false
	}

	escapedPath := appendEscapedSegment(plan.resinEscapedPath, plan.platformSegment)
	escapedPath = appendEscapedSegment(escapedPath, protocol)
	escapedPath = appendEscapedSegment(escapedPath, url.PathEscape(target.Host))
	if targetPath := exactEscapedPath(target); targetPath != "" {
		if !strings.HasPrefix(targetPath, "/") {
			escapedPath += "/"
		}
		escapedPath += targetPath
	}

	decodedPath, errUnescape := url.PathUnescape(escapedPath)
	if errUnescape != nil {
		return nil, false
	}

	return &url.URL{
		Scheme:     resinScheme,
		Host:       plan.resinHost,
		Path:       decodedPath,
		RawPath:    escapedPath,
		RawQuery:   target.RawQuery,
		ForceQuery: target.ForceQuery,
	}, true
}

func exactEscapedPath(value *url.URL) string {
	if value.RawPath == "" {
		return value.EscapedPath()
	}
	decodedPath, errUnescape := url.PathUnescape(value.RawPath)
	if errUnescape != nil || decodedPath != value.Path {
		return value.EscapedPath()
	}

	var escaped strings.Builder
	chunkStart := 0
	for index := 0; index < len(value.RawPath); {
		if value.RawPath[index] != '%' {
			index++
			continue
		}
		if chunkStart < index {
			escaped.WriteString((&url.URL{Path: value.RawPath[chunkStart:index]}).EscapedPath())
		}
		escaped.WriteString(value.RawPath[index : index+3])
		index += 3
		chunkStart = index
	}
	if chunkStart < len(value.RawPath) {
		escaped.WriteString((&url.URL{Path: value.RawPath[chunkStart:]}).EscapedPath())
	}
	return escaped.String()
}

func appendEscapedSegment(escapedPath, segment string) string {
	if escapedPath == "" {
		return "/" + segment
	}
	if strings.HasSuffix(escapedPath, "/") {
		return escapedPath + segment
	}
	return escapedPath + "/" + segment
}

func deleteHeaderCaseInsensitive(headers http.Header, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}

func unwrapTransport(transport *routingTransport) http.RoundTripper {
	var fallback http.RoundTripper = transport.fallback
	for {
		wrapped, ok := fallback.(*routingTransport)
		if !ok {
			return fallback
		}
		fallback = wrapped.fallback
	}
}

func (transport *routingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	routedURL, ok := routeURL(transport.plan, req.URL, false)
	if !ok {
		return fallbackTransport(transport.fallback).RoundTrip(req)
	}

	routedReq := req.Clone(req.Context())
	routedReq.URL = routedURL
	routedReq.Host = ""
	routedReq.RequestURI = ""
	if routedReq.Header == nil {
		routedReq.Header = make(http.Header)
	}
	deleteHeaderCaseInsensitive(routedReq.Header, "Host")
	deleteHeaderCaseInsensitive(routedReq.Header, accountHeader)
	routedReq.Header.Set(accountHeader, transport.plan.account)

	resp, errRoundTrip := directTransport.RoundTrip(routedReq)
	if resp != nil {
		resp.Request = req
	}
	return resp, errRoundTrip
}

func (transport *routingTransport) CloseIdleConnections() {
	if closer, ok := directTransport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}

	fallback := fallbackTransport(transport.fallback)
	if sameRoundTripper(directTransport, fallback) {
		return
	}
	if closer, ok := fallback.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func fallbackTransport(fallback http.RoundTripper) http.RoundTripper {
	if fallback == nil {
		return http.DefaultTransport
	}
	return fallback
}

func sameRoundTripper(left, right http.RoundTripper) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	return leftValue.Type() == rightValue.Type() && leftValue.Comparable() && leftValue.Equal(rightValue)
}

func websocketDialKey(target *url.URL, headers http.Header, cfg *config.Config, auth *cliproxyauth.Auth, plan routePlan, routed bool) string {
	hasher := sha256.New()
	writeDialKeyPart(hasher, "resin-websocket-v1")
	if routed {
		writeDialKeyPart(hasher, "routed")
	} else {
		writeDialKeyPart(hasher, "fallback")
	}
	if target == nil {
		writeDialKeyPart(hasher, "")
	} else {
		writeDialKeyPart(hasher, target.String())
	}
	if auth == nil {
		writeDialKeyPart(hasher, "")
	} else {
		writeDialKeyPart(hasher, auth.ID)
	}
	writeDialKeyPart(hasher, identityHeadersDigest(headers))

	if routed {
		writeDialKeyPart(hasher, plan.resinScheme)
		writeDialKeyPart(hasher, plan.resinHost)
		writeDialKeyPart(hasher, plan.resinEscapedPath)
		writeDialKeyPart(hasher, plan.platform)
		writeDialKeyPart(hasher, plan.account)
	} else {
		if auth == nil {
			writeDialKeyPart(hasher, "")
		} else {
			writeDialKeyPart(hasher, strings.TrimSpace(auth.ProxyURL))
		}
		if cfg == nil {
			writeDialKeyPart(hasher, "")
		} else {
			writeDialKeyPart(hasher, strings.TrimSpace(cfg.ProxyURL))
		}
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func identityHeadersDigest(headers http.Header) string {
	entries := make([]string, 0)
	for name, headerValues := range headers {
		entryHasher := sha256.New()
		writeDialKeyPart(entryHasher, strings.ToLower(name))
		for _, value := range headerValues {
			writeDialKeyPart(entryHasher, value)
		}
		entries = append(entries, hex.EncodeToString(entryHasher.Sum(nil)))
	}
	sort.Strings(entries)

	hasher := sha256.New()
	for _, entry := range entries {
		writeDialKeyPart(hasher, entry)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeDialKeyPart(hasher hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write([]byte(value))
}
