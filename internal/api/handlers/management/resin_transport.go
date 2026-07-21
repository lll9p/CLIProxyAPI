package management

import (
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/resin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// apiCallOutboundTransport wraps the raw api-call transport with Resin routing
// so management api-call requests (including quota refresh style calls) keep
// the same egress identity as runtime requests for eligible account auths.
// Ineligible auths keep the raw proxy/direct behavior unchanged.
func (h *Handler) apiCallOutboundTransport(auth *coreauth.Auth) http.RoundTripper {
	var cfg *config.Config
	if h != nil {
		cfg = h.cfg
	}
	return resin.WrapTransport(cfg, auth, h.apiCallTransport(auth))
}
