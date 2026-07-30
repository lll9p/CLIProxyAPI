package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestAPICallOutboundTransportWrapsEligibleAuthWithResin(t *testing.T) {
	t.Parallel()

	h := &Handler{cfg: &config.Config{SDKConfig: sdkconfig.SDKConfig{
		ResinURL:          "http://resin:2260/secret",
		ResinPlatformName: "cpa",
	}}}

	eligible := h.apiCallOutboundTransport(&coreauth.Auth{
		Provider: "codex",
		FileName: "codex-user.json",
		ProxyURL: "direct",
		Metadata: map[string]any{"access_token": "token"},
	})
	if _, ok := eligible.(*http.Transport); ok {
		t.Fatal("expected eligible auth transport to be wrapped with Resin routing")
	}

	ineligible := h.apiCallOutboundTransport(&coreauth.Auth{
		Provider:   "codex",
		FileName:   "codex-api-key.json",
		Attributes: map[string]string{"api_key": "sk-key"},
	})
	if _, ok := ineligible.(*http.Transport); !ok {
		t.Fatalf("ineligible auth transport = %T, want raw *http.Transport", ineligible)
	}
}

func TestAPICallRoutesEligibleAuthThroughResin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstreamServer.Close()
	upstreamHost := strings.TrimPrefix(upstreamServer.URL, "http://")

	var resinHost string
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.EscapedPath(), "/secret/cpa/http/"+upstreamHost+"/quota"; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		if got := req.Header.Get("X-Resin-Account"); got != "codex-user.json" {
			t.Errorf("X-Resin-Account = %q, want codex-user.json", got)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Errorf("Authorization = %q, want Bearer codex-token", got)
		}
		if req.Host != resinHost {
			t.Errorf("request Host = %q, want Resin host %q (custom Host override must not survive Resin routing)", req.Host, resinHost)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer resinServer.Close()
	resinHost = strings.TrimPrefix(resinServer.URL, "http://")

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "codex-user.json",
		Provider: "codex",
		FileName: "codex-user.json",
		Metadata: map[string]any{"access_token": "codex-token", "type": "codex"},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	authIndex := auth.EnsureIndex()

	h := &Handler{
		cfg: &config.Config{SDKConfig: sdkconfig.SDKConfig{
			ResinURL:          resinServer.URL + "/secret",
			ResinPlatformName: "cpa",
		}},
		authManager: manager,
	}

	payload := `{"auth_index":"` + authIndex + `","method":"GET","url":"` + upstreamServer.URL + `/quota","header":{"Authorization":"Bearer $TOKEN$","Host":"upstream.example.com"}}`
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", strings.NewReader(payload))
	ginCtx.Request.Header.Set("Content-Type", "application/json")

	h.APICall(ginCtx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("APICall status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"status_code":200`) {
		t.Fatalf("APICall response missing upstream status: %s", body)
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0 (api-call must route through Resin)", got)
	}
}
