package resin

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestStableAccount(t *testing.T) {
	tests := []struct {
		name string
		auth *cliproxyauth.Auth
		want string
	}{
		{name: "nil", want: ""},
		{name: "file name wins", auth: &cliproxyauth.Auth{FileName: "  C:\\auths\\user@example.com.json  ", ID: "fallback-id"}, want: "C:\\auths\\user@example.com.json"},
		{name: "id fallback", auth: &cliproxyauth.Auth{ID: "  stable-id  "}, want: "stable-id"},
		{name: "empty", auth: &cliproxyauth.Auth{}, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := stableAccount(test.auth); got != test.want {
				t.Fatalf("stableAccount() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSupportsAuth(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "antigravity", "gemini", "gemini-interactions", "kimi", "xai", "vertex"} {
		t.Run(provider, func(t *testing.T) {
			auth := oauthAuth(provider, "account.json")
			if !supportsAuth(auth) {
				t.Fatalf("supportsAuth(%q) = false, want true", provider)
			}
		})
	}

	for _, provider := range []string{"", "aistudio", "anthropic", "openai-compatibility", "custom"} {
		t.Run("unsupported_"+provider, func(t *testing.T) {
			if supportsAuth(oauthAuth(provider, "account.json")) {
				t.Fatalf("supportsAuth(%q) = true, want false", provider)
			}
		})
	}

	apiKey := oauthAuth("vertex", "vertex.json")
	apiKey.Attributes[cliproxyauth.AttributeAuthKind] = cliproxyauth.AuthKindAPIKey
	if supportsAuth(apiKey) {
		t.Fatal("supportsAuth(vertex API key) = true, want false")
	}
	if supportsAuth(nil) {
		t.Fatal("supportsAuth(nil) = true, want false")
	}
}

func TestNewRoutePlanValidation(t *testing.T) {
	validAuth := oauthAuth("codex", "account.json")
	tests := []struct {
		name     string
		cfg      *config.Config
		auth     *cliproxyauth.Auth
		wantOK   bool
		wantBase string
	}{
		{name: "http", cfg: resinConfig("http://resin:2260/ThePasswd", "cpa"), auth: validAuth, wantOK: true, wantBase: "/ThePasswd"},
		{name: "https", cfg: resinConfig("https://resin.example/base%2Fkey", "cpa"), auth: validAuth, wantOK: true, wantBase: "/base%2Fkey"},
		{name: "nil config", auth: validAuth},
		{name: "empty URL", cfg: resinConfig("", "cpa"), auth: validAuth},
		{name: "userinfo", cfg: resinConfig("http://user:pass@resin/base", "cpa"), auth: validAuth},
		{name: "query", cfg: resinConfig("http://resin/base?x=1", "cpa"), auth: validAuth},
		{name: "empty query", cfg: resinConfig("http://resin/base?", "cpa"), auth: validAuth},
		{name: "fragment", cfg: resinConfig("http://resin/base#fragment", "cpa"), auth: validAuth},
		{name: "empty fragment", cfg: resinConfig("http://resin/base#", "cpa"), auth: validAuth},
		{name: "opaque", cfg: resinConfig("http:opaque", "cpa"), auth: validAuth},
		{name: "empty host", cfg: resinConfig("http:///base", "cpa"), auth: validAuth},
		{name: "unsupported scheme", cfg: resinConfig("ws://resin/base", "cpa"), auth: validAuth},
		{name: "empty platform", cfg: resinConfig("http://resin/base", ""), auth: validAuth},
		{name: "platform slash", cfg: resinConfig("http://resin/base", "cp/a"), auth: validAuth},
		{name: "platform dot", cfg: resinConfig("http://resin/base", "cp.a"), auth: validAuth},
		{name: "platform colon", cfg: resinConfig("http://resin/base", "cp:a"), auth: validAuth},
		{name: "platform control", cfg: resinConfig("http://resin/base", "cp\na"), auth: validAuth},
		{name: "missing account", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "")},
		{name: "account CR", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "account\rvalue")},
		{name: "account tab", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "account\tvalue")},
		{name: "account DEL", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "account\x7fvalue")},
		{name: "account unicode control", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "account\u0085value")},
		{name: "account invalid UTF-8", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", "account\xffvalue")},
		{name: "account too long", cfg: resinConfig("http://resin/base", "cpa"), auth: oauthAuth("codex", strings.Repeat("a", maxAccountHeaderSize+1))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, ok := newRoutePlan(test.cfg, test.auth)
			if ok != test.wantOK {
				t.Fatalf("newRoutePlan() ok = %v, want %v", ok, test.wantOK)
			}
			if ok && plan.resinEscapedPath != test.wantBase {
				t.Fatalf("plan.resinEscapedPath = %q, want %q", plan.resinEscapedPath, test.wantBase)
			}
		})
	}
}

func TestRouteURLHTTPV1AndEscapedPath(t *testing.T) {
	plan := mustRoutePlan(t, resinConfig("http://resin:2260/The%2FPass//", "cpa"), oauthAuth("codex", "account.json"))
	target := mustURL(t, "https://example.com:8443/a%2fb/%25/a%20b/中文//./../tail/?b=2&a=1&a=3")
	original := *target

	routed, ok := routeURL(plan, target, false)
	if !ok {
		t.Fatal("routeURL() = false, want true")
	}
	want := "http://resin:2260/The%2FPass//cpa/https/example.com:8443/a%2fb/%25/a%20b/%E4%B8%AD%E6%96%87//./../tail/?b=2&a=1&a=3"
	if got := routed.String(); got != want {
		t.Fatalf("routeURL().String() = %q, want %q", got, want)
	}
	if routed.RawPath != "/The%2FPass//cpa/https/example.com:8443/a%2fb/%25/a%20b/%E4%B8%AD%E6%96%87//./../tail/" {
		t.Fatalf("routeURL().RawPath = %q", routed.RawPath)
	}
	if routed.RawQuery != target.RawQuery || routed.ForceQuery != target.ForceQuery {
		t.Fatalf("routeURL() query = %q/%v, want %q/%v", routed.RawQuery, routed.ForceQuery, target.RawQuery, target.ForceQuery)
	}
	if !reflect.DeepEqual(*target, original) {
		t.Fatalf("routeURL() mutated target: got %#v, want %#v", *target, original)
	}
	if strings.Contains(routed.EscapedPath(), "cpa:") {
		t.Fatalf("routeURL() emitted legacy identity: %q", routed.EscapedPath())
	}
}

func TestRouteURLWebSocket(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		target     string
		want       string
		wantRouted bool
	}{
		{
			name:       "ws maps to http",
			base:       "http://resin:2260/ThePasswd",
			target:     "ws://chatgpt.com/backend-api/codex/responses",
			want:       "ws://resin:2260/ThePasswd/cpa/http/chatgpt.com/backend-api/codex/responses",
			wantRouted: true,
		},
		{
			name:       "wss maps to https and preserves IPv6 port and force query",
			base:       "http://resin:2260/ThePasswd",
			target:     "wss://[2001:db8::1]:9443/a%2Fdeep?",
			want:       "ws://resin:2260/ThePasswd/cpa/https/%5B2001:db8::1%5D:9443/a%2Fdeep?",
			wantRouted: true,
		},
		{
			name:   "https Resin base does not infer wss",
			base:   "https://resin:2260/ThePasswd",
			target: "wss://chatgpt.com/responses",
		},
		{
			name:   "unsupported target scheme",
			base:   "http://resin:2260/ThePasswd",
			target: "https://chatgpt.com/responses",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := mustRoutePlan(t, resinConfig(test.base, "cpa"), oauthAuth("codex", "account.json"))
			routed, ok := routeURL(plan, mustURL(t, test.target), true)
			if ok != test.wantRouted {
				t.Fatalf("routeURL() ok = %v, want %v", ok, test.wantRouted)
			}
			if ok && routed.String() != test.want {
				t.Fatalf("routeURL().String() = %q, want %q", routed.String(), test.want)
			}
		})
	}
}

func TestRouteURLRejectsInvalidTargets(t *testing.T) {
	plan := mustRoutePlan(t, resinConfig("http://resin/base", "cpa"), oauthAuth("codex", "account.json"))
	tests := []*url.URL{
		nil,
		{Scheme: "https", Path: "/missing-host"},
		{Scheme: "https", Host: "example.com", Opaque: "opaque"},
		{Scheme: "ftp", Host: "example.com", Path: "/file"},
	}
	for _, target := range tests {
		if routed, ok := routeURL(plan, target, false); ok || routed != nil {
			t.Fatalf("routeURL(%#v) = %#v, %v; want nil, false", target, routed, ok)
		}
	}
}

func TestRouteURLDropsTargetUserinfoAndFragment(t *testing.T) {
	plan := mustRoutePlan(t, resinConfig("http://resin/base", "cpa"), oauthAuth("codex", "account.json"))
	target := mustURL(t, "https://user:pass@example.com/path?x=1#ignored")
	routed, ok := routeURL(plan, target, false)
	if !ok {
		t.Fatal("routeURL() = false, want true")
	}
	if routed.User != nil || routed.Fragment != "" || routed.RawFragment != "" {
		t.Fatalf("routeURL() retained userinfo or fragment: %#v", routed)
	}
	if got, want := routed.String(), "http://resin/base/cpa/https/example.com/path?x=1"; got != want {
		t.Fatalf("routeURL().String() = %q, want %q", got, want)
	}
}

func TestWrapTransportInactiveReturnsFallback(t *testing.T) {
	fallback := &recordingTransport{}
	tests := []struct {
		name string
		cfg  *config.Config
		auth *cliproxyauth.Auth
	}{
		{name: "nil config", auth: oauthAuth("codex", "account.json")},
		{name: "missing URL", cfg: resinConfig("", "cpa"), auth: oauthAuth("codex", "account.json")},
		{name: "unsupported provider", cfg: resinConfig("http://resin", "cpa"), auth: oauthAuth("aistudio", "account.json")},
		{name: "missing account", cfg: resinConfig("http://resin", "cpa"), auth: oauthAuth("codex", "")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := WrapTransport(test.cfg, test.auth, fallback); got != fallback {
				t.Fatalf("WrapTransport() = %T, want original fallback", got)
			}
		})
	}

	apiKey := oauthAuth("vertex", "vertex.json")
	apiKey.Attributes[cliproxyauth.AttributeAuthKind] = cliproxyauth.AuthKindAPIKey
	if got := WrapTransport(resinConfig("http://resin", "cpa"), apiKey, fallback); got != fallback {
		t.Fatalf("WrapTransport(API key) = %T, want original fallback", got)
	}
	if got := WrapTransport(nil, nil, nil); got != nil {
		t.Fatalf("WrapTransport(nil) = %T, want nil", got)
	}
}

func TestRoutingTransportRoundTrip(t *testing.T) {
	type receivedRequest struct {
		path       string
		rawQuery   string
		host       string
		account    []string
		hostHeader []string
		body       string
	}
	received := make(chan receivedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Errorf("read request body: %v", errRead)
		}
		received <- receivedRequest{
			path:       req.URL.EscapedPath(),
			rawQuery:   req.URL.RawQuery,
			host:       req.Host,
			account:    headerValuesCaseInsensitive(req.Header, accountHeader),
			hostHeader: headerValuesCaseInsensitive(req.Header, "Host"),
			body:       string(body),
		}
		writer.Header().Set("X-Upstream", "resin")
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := resinConfig(server.URL+"/ThePasswd", "cpa")
	cfg.ProxyURL = "http://global-proxy.invalid:8080"
	auth := oauthAuth("codex", " C:\\auths\\user@example.com.json ")
	auth.ProxyURL = "socks5://auth-proxy.invalid:1080"
	fallback := &recordingTransport{}
	transport := WrapTransport(cfg, auth, fallback)
	target := mustURL(t, "https://upstream.example:9443/a%2Fb//tail?x=1&x=2")
	req, errRequest := http.NewRequestWithContext(context.Background(), http.MethodPost, target.String(), strings.NewReader("payload"))
	if errRequest != nil {
		t.Fatalf("new request: %v", errRequest)
	}
	req.URL.RawPath = target.RawPath
	req.Host = "upstream-override.example"
	req.RequestURI = "https://upstream.example:9443/should-be-cleared"
	req.Header = http.Header{
		"Authorization":   {"Bearer secret-token"},
		"HOST":            {"header-override.example"},
		"x-resin-account": {"stale-lower"},
		"X-RESIN-ACCOUNT": {"stale-upper"},
	}
	req.Trailer = http.Header{"X-Trailer": {"trailer-value"}}

	originalURL := *req.URL
	originalHeaders := req.Header.Clone()
	originalTrailer := req.Trailer.Clone()
	originalHost := req.Host
	originalRequestURI := req.RequestURI
	originalBody := req.Body
	originalGetBody := reflect.ValueOf(req.GetBody).Pointer()

	resp, errRoundTrip := transport.RoundTrip(req)
	if errRoundTrip != nil {
		t.Fatalf("RoundTrip() error = %v", errRoundTrip)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			t.Errorf("close response body: %v", errClose)
		}
	}()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if resp.Request != req {
		t.Fatal("response.Request was not restored to the original request")
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallback.calls)
	}

	got := <-received
	if got.path != "/ThePasswd/cpa/https/upstream.example:9443/a%2Fb//tail" {
		t.Fatalf("received path = %q", got.path)
	}
	if got.rawQuery != "x=1&x=2" {
		t.Fatalf("received query = %q", got.rawQuery)
	}
	resinHost := strings.TrimPrefix(server.URL, "http://")
	if got.host != resinHost {
		t.Fatalf("received Host = %q, want %q", got.host, resinHost)
	}
	if !reflect.DeepEqual(got.account, []string{"C:\\auths\\user@example.com.json"}) {
		t.Fatalf("received account header = %#v", got.account)
	}
	if len(got.hostHeader) != 0 {
		t.Fatalf("received Host header values = %#v, want none", got.hostHeader)
	}
	if got.body != "payload" {
		t.Fatalf("received body = %q, want payload", got.body)
	}

	if !reflect.DeepEqual(*req.URL, originalURL) || !reflect.DeepEqual(req.Header, originalHeaders) || !reflect.DeepEqual(req.Trailer, originalTrailer) || req.Host != originalHost || req.RequestURI != originalRequestURI {
		t.Fatal("RoundTrip() mutated the original request")
	}
	if req.Body != originalBody || reflect.ValueOf(req.GetBody).Pointer() != originalGetBody {
		t.Fatal("RoundTrip() replaced Body or GetBody on the original request")
	}
}

func TestRoutingTransportPreservesRedirectAndBodyReplay(t *testing.T) {
	type receivedRequest struct {
		path string
		body string
	}
	received := make(chan receivedRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Errorf("read request body: %v", errRead)
		}
		received <- receivedRequest{path: req.URL.EscapedPath(), body: string(body)}
		if strings.HasSuffix(req.URL.Path, "/start") {
			writer.Header().Set("Location", "/next")
			writer.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &http.Client{Transport: WrapTransport(
		resinConfig(server.URL+"/ThePasswd", "cpa"),
		oauthAuth("codex", "account.json"),
		http.DefaultTransport,
	)}
	req, errRequest := http.NewRequest(http.MethodPost, "https://upstream.example/start", strings.NewReader("payload"))
	if errRequest != nil {
		t.Fatalf("new request: %v", errRequest)
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		t.Fatalf("client.Do() error = %v", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			t.Errorf("close response body: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Request.URL.String(); got != "https://upstream.example/next" {
		t.Fatalf("response request URL = %q, want logical upstream redirect URL", got)
	}
	first := <-received
	second := <-received
	if first.path != "/ThePasswd/cpa/https/upstream.example/start" || second.path != "/ThePasswd/cpa/https/upstream.example/next" {
		t.Fatalf("received paths = %q, %q", first.path, second.path)
	}
	if first.body != "payload" || second.body != "payload" {
		t.Fatalf("received bodies = %q, %q; want replayed payload", first.body, second.body)
	}
}

func TestRoutingTransportInitializesNilHeader(t *testing.T) {
	originalDirect := directTransport
	defer func() { directTransport = originalDirect }()

	direct := &recordingTransport{}
	directTransport = direct
	transport := WrapTransport(resinConfig("http://resin/base", "cpa"), oauthAuth("codex", "account.json"), nil)
	req := &http.Request{Method: http.MethodGet, URL: mustURL(t, "https://example.com/path")}

	resp, errRoundTrip := transport.RoundTrip(req)
	if errRoundTrip != nil {
		t.Fatalf("RoundTrip() error = %v", errRoundTrip)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("close response body: %v", errClose)
	}
	if req.Header != nil {
		t.Fatalf("RoundTrip() changed original nil Header to %#v", req.Header)
	}
	if direct.lastRequest == req {
		t.Fatal("RoundTrip() did not clone the request")
	}
	if got := direct.lastRequest.Header.Values(accountHeader); !reflect.DeepEqual(got, []string{"account.json"}) {
		t.Fatalf("routed account header = %#v", got)
	}
}

func TestRoutingTransportFallsBackWithoutMutation(t *testing.T) {
	fallback := &recordingTransport{response: &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}}
	transport := WrapTransport(resinConfig("http://resin", "cpa"), oauthAuth("codex", "account.json"), fallback)
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        mustURL(t, "ftp://example.com/file"),
		Header:     http.Header{"x-resin-account": {"caller-value"}},
		Host:       "caller-host",
		RequestURI: "ftp://example.com/file",
	}
	originalURL := req.URL
	originalHeaders := req.Header.Clone()

	resp, errRoundTrip := transport.RoundTrip(req)
	if errRoundTrip != nil {
		t.Fatalf("RoundTrip() error = %v", errRoundTrip)
	}
	if resp.StatusCode != http.StatusNoContent || fallback.calls != 1 || fallback.lastRequest != req {
		t.Fatalf("fallback result = status %d, calls %d, request %p; want status 204, calls 1, request %p", resp.StatusCode, fallback.calls, fallback.lastRequest, req)
	}
	if req.URL != originalURL || !reflect.DeepEqual(req.Header, originalHeaders) || req.Host != "caller-host" || req.RequestURI != "ftp://example.com/file" {
		t.Fatal("fallback path mutated the request")
	}
}

func TestWrapTransportPlanSnapshotAndIdempotence(t *testing.T) {
	cfg := resinConfig("http://resin/one", "cpa")
	auth := oauthAuth("codex", "one.json")
	fallback := &recordingTransport{}

	first := WrapTransport(cfg, auth, fallback)
	if again := WrapTransport(cfg, auth, first); again != first {
		t.Fatal("same route plan added another wrapper")
	}

	firstPlan := first.(*routingTransport).plan
	cfg.ResinURL = "http://resin/two"
	cfg.ResinPlatformName = "new-platform"
	auth.FileName = "two.json"
	if first.(*routingTransport).plan != firstPlan {
		t.Fatal("existing wrapper retained mutable config or auth state")
	}

	second := WrapTransport(cfg, auth, first)
	secondTransport, ok := second.(*routingTransport)
	if !ok {
		t.Fatalf("WrapTransport() = %T, want *routingTransport", second)
	}
	if second == first {
		t.Fatal("different route plan reused old wrapper")
	}
	if secondTransport.fallback != fallback {
		t.Fatalf("different route plan fallback = %T, want original fallback", secondTransport.fallback)
	}
	if secondTransport.plan.account != "two.json" || secondTransport.plan.platform != "new-platform" || secondTransport.plan.resinEscapedPath != "/two" {
		t.Fatalf("new plan = %#v", secondTransport.plan)
	}
}

func TestRoutingTransportCloseIdleConnections(t *testing.T) {
	originalDirect := directTransport
	defer func() { directTransport = originalDirect }()

	direct := &recordingTransport{}
	fallback := &recordingTransport{}
	directTransport = direct
	transport := WrapTransport(resinConfig("http://resin", "cpa"), oauthAuth("codex", "account.json"), fallback)
	transport.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	if direct.closeCalls != 1 || fallback.closeCalls != 1 {
		t.Fatalf("close calls direct/fallback = %d/%d, want 1/1", direct.closeCalls, fallback.closeCalls)
	}

	direct.closeCalls = 0
	directTransport = direct
	transport = WrapTransport(resinConfig("http://resin", "cpa"), oauthAuth("codex", "account.json"), direct)
	transport.(interface{ CloseIdleConnections() }).CloseIdleConnections()
	if direct.closeCalls != 1 {
		t.Fatalf("same direct/fallback close calls = %d, want 1", direct.closeCalls)
	}
}

func TestDirectTransportBypassesProxy(t *testing.T) {
	transport, ok := directTransport.(*http.Transport)
	if !ok {
		t.Fatalf("directTransport = %T, want *http.Transport", directTransport)
	}
	if transport.Proxy != nil {
		t.Fatal("directTransport.Proxy is non-nil")
	}
}

func TestPrepareWebSocketRouted(t *testing.T) {
	cfg := resinConfig("http://resin:2260/ThePasswd", "cpa")
	auth := oauthAuth("codex", "account@example.com.json")
	auth.ID = "auth-id"
	target := mustURL(t, "wss://chatgpt.com/backend-api/codex/responses?foo=bar&foo=baz")
	originalTarget := *target
	headers := http.Header{
		"Authorization":   {"Bearer secret-token"},
		"Cookie":          {"session=secret-cookie"},
		"HOST":            {"upstream.example"},
		"x-resin-account": {"stale-lower"},
		"X-RESIN-ACCOUNT": {"stale-upper"},
		"OpenAI-Beta":     {"responses_websockets=2026-02-06"},
	}
	originalHeaders := headers.Clone()

	dialURL, dialHeaders, dialKey, routed := PrepareWebSocket(cfg, auth, target, headers)
	if !routed {
		t.Fatal("PrepareWebSocket() routed = false, want true")
	}
	wantURL := "ws://resin:2260/ThePasswd/cpa/https/chatgpt.com/backend-api/codex/responses?foo=bar&foo=baz"
	if dialURL.String() != wantURL {
		t.Fatalf("dial URL = %q, want %q", dialURL.String(), wantURL)
	}
	if got := headerValuesCaseInsensitive(dialHeaders, accountHeader); !reflect.DeepEqual(got, []string{"account@example.com.json"}) {
		t.Fatalf("account headers = %#v", got)
	}
	if got := headerValuesCaseInsensitive(dialHeaders, "Host"); len(got) != 0 {
		t.Fatalf("Host headers = %#v, want none", got)
	}
	if !reflect.DeepEqual(headerValuesCaseInsensitive(dialHeaders, "Authorization"), []string{"Bearer secret-token"}) ||
		!reflect.DeepEqual(headerValuesCaseInsensitive(dialHeaders, "Cookie"), []string{"session=secret-cookie"}) ||
		!reflect.DeepEqual(headerValuesCaseInsensitive(dialHeaders, "OpenAI-Beta"), []string{"responses_websockets=2026-02-06"}) {
		t.Fatalf("routed headers lost caller values: %#v", dialHeaders)
	}
	dialHeaders.Set("Authorization", "changed")
	if !reflect.DeepEqual(headers, originalHeaders) {
		t.Fatal("PrepareWebSocket() mutated caller headers")
	}
	if !reflect.DeepEqual(*target, originalTarget) {
		t.Fatal("PrepareWebSocket() mutated target URL")
	}
	assertOpaqueKey(t, dialKey, "secret-token", "secret-cookie", "account@example.com.json", wantURL)
}

func TestPrepareWebSocketFallback(t *testing.T) {
	cfg := resinConfig("https://resin:2260/ThePasswd", "cpa")
	cfg.ProxyURL = "http://global-proxy:8080"
	auth := oauthAuth("codex", "account.json")
	auth.ID = "auth-id"
	auth.ProxyURL = "socks5://auth-proxy:1080"
	target := mustURL(t, "wss://chatgpt.com/responses")
	headers := http.Header{"Authorization": {"Bearer fallback-secret"}}

	dialURL, dialHeaders, dialKey, routed := PrepareWebSocket(cfg, auth, target, headers)
	if routed {
		t.Fatal("PrepareWebSocket() routed = true for HTTPS Resin base")
	}
	if dialURL != target {
		t.Fatal("fallback dial URL is not the original target")
	}
	if !reflect.DeepEqual(dialHeaders, headers) {
		t.Fatalf("fallback headers = %#v, want %#v", dialHeaders, headers)
	}
	if strings.HasPrefix(dialURL.String(), "wss://resin") {
		t.Fatalf("fallback inferred Resin wss URL: %q", dialURL)
	}
	assertOpaqueKey(t, dialKey, "fallback-secret", auth.ProxyURL, cfg.ProxyURL, target.String())
}

func TestPrepareWebSocketDialKeyStabilityAndInputs(t *testing.T) {
	cfg := resinConfig("http://resin:2260/base", "cpa")
	auth := oauthAuth("codex", "account.json")
	auth.ID = "auth-one"
	target := mustURL(t, "wss://chatgpt.com/responses")
	headers := http.Header{
		"Authorization":      {"Bearer token"},
		"ChatGPT-Account-ID": {"account-one"},
		"Cookie":             {"b=2", "a=1"},
		"OpenAI-Beta":        {"responses_websockets=2026-02-06"},
	}
	_, _, baseKey, routed := PrepareWebSocket(cfg, auth, target, headers)
	if !routed {
		t.Fatal("base PrepareWebSocket() routed = false")
	}

	reorderedHeaders := http.Header{
		"openai-beta":        {"responses_websockets=2026-02-06"},
		"cookie":             {"b=2", "a=1"},
		"chatgpt-account-id": {"account-one"},
		"authorization":      {"Bearer token"},
	}
	_, _, stableKey, _ := PrepareWebSocket(cfg, auth, target, reorderedHeaders)
	if stableKey != baseKey {
		t.Fatalf("semantically equivalent identity headers changed key: %q != %q", stableKey, baseKey)
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		auth    *cliproxyauth.Auth
		target  *url.URL
		headers http.Header
	}{
		{name: "target", cfg: cfg, auth: auth, target: mustURL(t, "wss://chatgpt.com/other"), headers: headers},
		{name: "base", cfg: resinConfig("http://other-resin/base", "cpa"), auth: auth, target: target, headers: headers},
		{name: "platform", cfg: resinConfig("http://resin:2260/base", "other"), auth: auth, target: target, headers: headers},
		{name: "account", cfg: cfg, auth: cloneAuth(auth, "other.json", auth.ID, auth.ProxyURL), target: target, headers: headers},
		{name: "auth ID", cfg: cfg, auth: cloneAuth(auth, auth.FileName, "auth-two", auth.ProxyURL), target: target, headers: headers},
		{name: "authorization", cfg: cfg, auth: auth, target: target, headers: cloneHeadersWith(headers, "Authorization", "Bearer other")},
		{name: "cookie", cfg: cfg, auth: auth, target: target, headers: cloneHeadersWith(headers, "Cookie", "c=3")},
		{name: "account header", cfg: cfg, auth: auth, target: target, headers: cloneHeadersWith(headers, "ChatGPT-Account-ID", "account-two")},
		{name: "custom header", cfg: cfg, auth: auth, target: target, headers: cloneHeadersWith(headers, "OpenAI-Beta", "other")},
		{name: "route mode", cfg: resinConfig("https://resin:2260/base", "cpa"), auth: auth, target: target, headers: headers},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, gotKey, _ := PrepareWebSocket(test.cfg, test.auth, test.target, test.headers)
			if gotKey == baseKey {
				t.Fatalf("%s change did not change dial key", test.name)
			}
			assertOpaqueKey(t, gotKey, "Bearer token", "account.json", "auth-one")
		})
	}
}

func TestPrepareWebSocketFallbackDialKeyInputs(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://global-one:8080"}}
	auth := oauthAuth("aistudio", "account.json")
	auth.ID = "auth-one"
	auth.ProxyURL = "socks5://auth-one:1080"
	target := mustURL(t, "wss://example.com/ws")
	headers := http.Header{"Authorization": {"Bearer token"}, "Cookie": {"session=one"}}
	_, _, baseKey, routed := PrepareWebSocket(cfg, auth, target, headers)
	if routed {
		t.Fatal("unsupported provider unexpectedly routed")
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		auth    *cliproxyauth.Auth
		target  *url.URL
		headers http.Header
	}{
		{name: "target", cfg: cfg, auth: auth, target: mustURL(t, "wss://example.com/other"), headers: headers},
		{name: "auth proxy", cfg: cfg, auth: cloneAuth(auth, auth.FileName, auth.ID, "socks5://auth-two:1080"), target: target, headers: headers},
		{name: "global proxy", cfg: &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://global-two:8080"}}, auth: auth, target: target, headers: headers},
		{name: "auth ID", cfg: cfg, auth: cloneAuth(auth, auth.FileName, "auth-two", auth.ProxyURL), target: target, headers: headers},
		{name: "authorization", cfg: cfg, auth: auth, target: target, headers: http.Header{"Authorization": {"Bearer other"}, "Cookie": {"session=one"}}},
		{name: "cookie", cfg: cfg, auth: auth, target: target, headers: http.Header{"Authorization": {"Bearer token"}, "Cookie": {"session=two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, gotKey, gotRouted := PrepareWebSocket(test.cfg, test.auth, test.target, test.headers)
			if gotRouted {
				t.Fatal("fallback key test unexpectedly routed")
			}
			if gotKey == baseKey {
				t.Fatalf("%s change did not change fallback dial key", test.name)
			}
		})
	}

	_, _, nilKeyOne, nilRoutedOne := PrepareWebSocket(nil, nil, nil, nil)
	_, _, nilKeyTwo, nilRoutedTwo := PrepareWebSocket(nil, nil, nil, nil)
	if nilRoutedOne || nilRoutedTwo || nilKeyOne == "" || nilKeyOne != nilKeyTwo {
		t.Fatalf("nil fallback keys = %q/%q, routed = %v/%v", nilKeyOne, nilKeyTwo, nilRoutedOne, nilRoutedTwo)
	}
}

type recordingTransport struct {
	calls       int
	closeCalls  int
	lastRequest *http.Request
	response    *http.Response
	err         error
}

func (transport *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport.calls++
	transport.lastRequest = req
	if transport.response != nil || transport.err != nil {
		return transport.response, transport.err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func (transport *recordingTransport) CloseIdleConnections() {
	transport.closeCalls++
}

func oauthAuth(provider, fileName string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		ID:       strings.TrimSpace(fileName),
		Provider: provider,
		FileName: fileName,
		Attributes: map[string]string{
			cliproxyauth.AttributeAuthKind: cliproxyauth.AuthKindOAuth,
		},
	}
}

func cloneAuth(auth *cliproxyauth.Auth, fileName, id, proxyURL string) *cliproxyauth.Auth {
	clone := *auth
	clone.FileName = fileName
	clone.ID = id
	clone.ProxyURL = proxyURL
	clone.Attributes = make(map[string]string, len(auth.Attributes))
	for key, value := range auth.Attributes {
		clone.Attributes[key] = value
	}
	return &clone
}

func cloneHeadersWith(headers http.Header, name, value string) http.Header {
	clone := headers.Clone()
	clone.Set(name, value)
	return clone
}

func resinConfig(resinURL, platform string) *config.Config {
	return &config.Config{SDKConfig: config.SDKConfig{ResinURL: resinURL, ResinPlatformName: platform}}
}

func mustRoutePlan(t *testing.T, cfg *config.Config, auth *cliproxyauth.Auth) routePlan {
	t.Helper()
	plan, ok := newRoutePlan(cfg, auth)
	if !ok {
		t.Fatalf("newRoutePlan(%q, %q) = false", cfg.ResinURL, cfg.ResinPlatformName)
	}
	return plan
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, errParse := url.Parse(raw)
	if errParse != nil {
		t.Fatalf("parse URL %q: %v", raw, errParse)
	}
	return parsed
}

func headerValuesCaseInsensitive(headers http.Header, name string) []string {
	var values []string
	for key, headerValues := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, headerValues...)
		}
	}
	return values
}

func assertOpaqueKey(t *testing.T, key string, secrets ...string) {
	t.Helper()
	if len(key) != sha256HexSize {
		t.Fatalf("dial key length = %d, want %d", len(key), sha256HexSize)
	}
	if _, errDecode := hex.DecodeString(key); errDecode != nil {
		t.Fatalf("dial key is not hexadecimal: %v", errDecode)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(key, secret) {
			t.Fatalf("dial key leaks plaintext %q", secret)
		}
	}
}

const sha256HexSize = 64
