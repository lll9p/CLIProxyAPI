# Resin 泛化改造最小实施说明

本文以当前 `main` 分支和最新版 Resin V1 为基线。目标是在尽量小的修改面内，为已经持久化的账号型 provider runtime 出网请求增加 Resin reverse proxy routing。

Resin 不是普通 forward proxy，也不是 `ProxyURL` 的一种取值。本方案只把 Resin 固定在受支持账号的 runtime HTTP/WebSocket 出口外层，避免污染普通代理、management、translator、插件通用 HTTP bridge 和 SDK 通用 HTTP 请求。

本方案明确采用以下部署配置：

```yaml
resin-url: "http://resin:2260/ThePasswd"
resin-platform-name: "cpa"
```

Resin 服务与 CLIProxyAPI 位于受信任的自有网络中。WebSocket client-to-Resin 只使用 `ws`，不实现或推导 `wss`。

## main 基线

`main` 中没有 Resin 支持：

- `internal/config/sdk_config.go` 没有 `ResinURL` 和 `ResinPlatformName`。
- `config.example.yaml` 没有 `resin-url` 和 `resin-platform-name`。
- `internal/resin/` 不存在。

`main` 中已经足够集中的出网入口：

- `internal/runtime/executor/helps/proxy_helpers.go` 的 `NewProxyAwareHTTPClient` 覆盖大量普通 runtime HTTP 请求。
- `internal/runtime/executor/helps/utls_client.go` 的 `NewUtlsHTTPClient` 覆盖 Codex/Claude 等需要 uTLS 的 runtime HTTP 请求。
- `internal/runtime/executor/antigravity_executor.go` 的 `newAntigravityHTTPClient` 是 Antigravity 的集中 HTTP client 工厂。
- `internal/runtime/executor/codex_websockets_executor.go` 里定义的 `newProxyAwareWebsocketDialer` 已被 Codex 和 xAI WebSocket 共用。
- `cmd/fetch_codex_models` 自己创建 HTTP client，不经过 executor helper。
- `cmd/fetch_antigravity_models` 自己创建 HTTP client，不经过 executor helper。
- `sdk/cliproxy/antigravity_models.go` 自己创建 HTTP client，不经过 executor helper。
- `sdk/auth/filestore.go` 当前会在列举缺少 `project_id` 的 Antigravity auth 时通过 `http.DefaultClient` 隐式联网，必须移除这个副作用。
- `internal/pluginhost/http_bridge.go` 的 `hostHTTPClient.doHTTP` 是插件宿主暴露给插件的通用 HTTP bridge，不能被 Resin 自动改写。
- `internal/api/handlers/management/api_tools.go` 是 management 手工 `api-call`，不能被 Resin 自动改写。

## 实施目标

满足全部条件时，支持的账号型 provider runtime HTTP/WebSocket 请求走 Resin：

- `resin-url` 已配置。
- `resin-platform-name` 已配置。
- `auth` 通过 Resin allowlist 和 credential kind 检查。
- `auth.AuthKind()` 不是 `cliproxyauth.AuthKindAPIKey`。
- 能从 `auth.FileName` 或 `auth.ID` 得到稳定 account。
- HTTP 目标 URL scheme 是 `http` 或 `https`。
- WebSocket 目标 URL scheme 是 `ws` 或 `wss`。
- WebSocket 使用的 `resin-url` scheme 是 `http`。

Resin 生效后：

- 直接连接 Resin 服务。
- 不走 `auth.ProxyURL`。
- 不走 `cfg.ProxyURL`。
- 不走环境 proxy。
- 不走 upstream uTLS transport。
- 不走 Antigravity upstream HTTP/1.1 模拟层。
- 请求日志仍记录原始 upstream URL。

Resin 条件不满足时，必须保持 `main` 原行为。

## 明确不做

本方案禁止扩大修改面：

- 不修改 `sdk/proxyutil.BuildHTTPTransport` 或 `BuildDialer` 语义。
- 不修改 `internal/util/proxy.go` 的 `SetProxy` 语义。
- 不把 `resin-url` 塞进 `ProxyURL`。
- 不修改 `internal/translator/`。
- 不给所有 HTTP callsite 新增 provider 参数。
- 不让 management `api-call` 自动走 Resin。
- 不新增 management Resin CRUD endpoint。
- 不新增 management 专用 Resin 配置响应模型；完整 `/config` 和 `/config.yaml` 会自然包含新字段。
- 不修改 management UI。
- 不修改 watcher diff 日志。
- 不修改 README/README_CN/README_JA 来新增 Resin 配置说明。
- 不修改 CI 脚本。
- 不移动 `newProxyAwareWebsocketDialer` 文件。
- 不让插件通用 HTTP bridge 自动走 Resin。
- 不让各 executor 的通用 `HttpRequest` 自动走 Resin。
- 不从 auth metadata、`AccountInfo()`、API key、access token、refresh token、id token、cookie 或 provider subject 派生 Resin account。
- 不兼容 Resin `LEGACY_V0`，不为旧版 identity 格式增加分支。
- 不实现 client-to-Resin `wss`。

## 配置

### `internal/config/sdk_config.go`

在 `SDKConfig` 的 `ProxyURL` 后新增：

```go
// ResinURL is the base URL of the Resin reverse proxy for supported account provider traffic.
ResinURL string `yaml:"resin-url" json:"resin-url"`

// ResinPlatformName identifies this proxy instance/platform to Resin for sticky account routing.
ResinPlatformName string `yaml:"resin-platform-name" json:"resin-platform-name"`
```

`Config` 已内联 `SDKConfig`，业务代码直接使用 `cfg.ResinURL` 和 `cfg.ResinPlatformName`。`CloneForRuntime` 是反射克隆，不需要额外改 clone 代码。

### `config.example.yaml`

只在 `proxy-url` 附近加示例配置：

```yaml
# Resin reverse proxy base URL for supported account provider runtime traffic.
resin-url: ""

# Resin platform name used in route paths and sticky account routing.
resin-platform-name: ""
```

实际部署值为：

```yaml
resin-url: "http://resin:2260/ThePasswd"
resin-platform-name: "cpa"
```

本方案不包含 README/UI/management 专用配置页面同步。完整 management 配置接口会因为 `Config` 内联 `SDKConfig` 而自然展示和保存这两个字段，不需要额外代码。

Home 模式下配置所有权保持现状：远端完整配置覆盖本地 runtime 配置，因此 Resin 字段也由远端配置决定。Watcher diff 不新增 Resin 专用日志，但配置 reload 仍会创建使用新配置的 executor/client。

## `internal/resin` 最小 API

新增 `internal/resin`，作为唯一 canonical Resin implementation。

跨包公共 API 只保留两个函数：

```go
func WrapTransport(cfg *config.Config, auth *cliproxyauth.Auth, fallback http.RoundTripper) http.RoundTripper
func PrepareWebSocket(
    cfg *config.Config,
    auth *cliproxyauth.Auth,
    target *url.URL,
    headers http.Header,
) (dialURL *url.URL, dialHeaders http.Header, dialKey string, routed bool)
```

其余逻辑保持包内私有：

```go
const accountHeader = "X-Resin-Account"

type routePlan struct { /* immutable snapshot */ }

func newRoutePlan(cfg *config.Config, auth *cliproxyauth.Auth) (routePlan, bool)
func stableAccount(auth *cliproxyauth.Auth) string
func supportsAuth(auth *cliproxyauth.Auth) bool
func routeURL(plan routePlan, target *url.URL, websocket bool) (*url.URL, bool)
func deleteHeaderCaseInsensitive(headers http.Header, name string)
```

不要导出 account header，也不要让 WebSocket caller 自行修改 Resin headers。不要暴露 `WrapClient`、`DirectTransport`、`StableAccount`、`SupportsAuth`、`RouteHTTP` 或 `ApplyHTTP`。这些名字一旦导出就会扩大外部依赖面，后续很难收回。

不要再额外做 account-only wrapper，例如 `WrapTransportForAccount`。refresh 由 executor 传入完整 `auth` 创建 Resin-aware HTTP client，再注入 auth helper。

`internal/resin` 自身单测使用 `package resin`，可以直接覆盖私有函数；跨包测试只验证公开行为。

`WrapTransport` 和 `PrepareWebSocket` 必须在调用时从 `cfg` 和 `auth` 生成不可变 `routePlan`。transport/session 不能长期持有可变的 `cfg` 或 `auth` 指针；配置热更新通过创建新 wrapper 和比较 WebSocket `dialKey` 生效。

## Stable Account

私有 `stableAccount` 按原方案直接使用稳定的本地文件级标识：

```go
func stableAccount(auth *cliproxyauth.Auth) string {
    if auth == nil {
        return ""
    }
    if account := strings.TrimSpace(auth.FileName); account != "" {
        return account
    }
    return strings.TrimSpace(auth.ID)
}
```

不要对 `FileName` 或 `ID` 做 SHA/hash、basename 转换或其他匿名化。除 `strings.TrimSpace` 外，发送值必须保持原样。`FileName` 可能本身包含 email 或绝对路径；在本方案的受信任自有 Resin 部署中，这是明确接受的行为。

仍然必须验证 account 是合法且大小合理的 HTTP header value：空值、CR/LF、其他控制字符或超过实现上限的值应令 Resin route fallback，不能修改原请求。禁止从 auth metadata、`AccountInfo()`、token、cookie 或 provider subject 另行构造 account。

## Provider Allowlist

私有 `supportsAuth` 先排除 API-key auth，再检查 provider allowlist：

```go
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
```

Provider allowlist 只允许：

```text
codex
claude
antigravity
gemini
gemini-interactions
kimi
xai
vertex
```

`gemini-interactions` 是 Gemini native Interactions provider，也按 Gemini 纳入。AI Studio 的 provider identifier 是 `aistudio`，仍不纳入 allowlist。`anthropic` 不是当前原生 Claude provider，不纳入 allowlist。

`vertex-api-key` 等第三方兼容配置虽然可能生成 `Provider: "vertex"`，但 `AuthKind()` 会识别为 `AuthKindAPIKey`，必须 fallback 原行为。provider 缺失、unsupported 或 API-key auth 都不能创建 routing wrapper。

不要给 `NewProxyAwareHTTPClient` 或各 executor HTTP callsite 增加 provider 参数。

## Route 格式

HTTP 和 WebSocket 必须复用同一个私有 `routeURL` helper，只在目标协议映射和 Resin 连接 scheme 上分叉，避免复制路径拼接逻辑。

本方案只支持最新版 Resin V1 identity 解析。identity path segment 直接使用 `<resin-platform-name>`，account 通过 `X-Resin-Account` header 传递，不添加兼容 `LEGACY_V0` 的冒号后缀。

HTTP route：

```text
<resin-url>/<resin-platform-name>/<target-protocol>/<target-host>/<target-path>?<target-query>
```

示例：

```text
resin-url: http://resin:2260/ThePasswd
resin-platform-name: cpa
target: https://chatgpt.com/backend-api/codex/responses?foo=bar

result: http://resin:2260/ThePasswd/cpa/https/chatgpt.com/backend-api/codex/responses?foo=bar
```

规则：

- HTTP route 的 `resin-url` 只接受 hierarchical `http` 或 `https` URL、非空 `Host`，并拒绝 userinfo、query、fragment、`Opaque` URL。
- WebSocket route 只接受 `http` Resin base，并把 client-to-Resin scheme 改为 `ws`；`https` Resin base 不路由 WebSocket，也不尝试 `wss`。
- 保留 `resin-url` 的 scheme、`Host` 和 escaped base path。
- `resin-platform-name` 必须是单个非空 V1 identity segment；拒绝 `/`、`.`、`:`、控制字符及会造成 identity 歧义的值。`cpa` 是合法值。
- 目标协议 `http` 或 `https` 作为 path segment 追加。
- WebSocket 目标协议按 `ws -> http`、`wss -> https` 写入 path。
- 目标 host 使用 `target.Host`，作为单个 escaped path segment 追加；必须保留端口和 IPv6 方括号，不能使用 `Hostname()`。
- 路径构造只能使用 `base.EscapedPath()` 和 `target.EscapedPath()`，不能使用 `path.Join`、`url.JoinPath`、`ResolveReference` 或直接拼 decoded `Path`。
- 只增加 route 所需的分隔 `/`，不得清理目标重复斜杠、dot segments、trailing slash 或 percent-escape 大小写。
- 生成完整 escaped route path 后，用 `url.PathUnescape` 得到 `Path`，并把原 escaped 值写入 `RawPath`，保证 `URL.String()` 保留 `%2F`、`%25` 等原始语义。
- 原样复制目标 `RawQuery` 和 `ForceQuery`；target fragment 不发送；target `Opaque` 或空 `Host` 时 fallback。最新版 Resin 服务端仍可能把空 query 的 `/path?` 归一化为 `/path`，这是上游协议限制，不在客户端侧伪造解决。
- 路由 URL 不复制 target userinfo。进入 RoundTripper 前由 `http.Client` 生成的 Authorization header 保持不变。
- 配置缺失、account 缺失、auth unsupported、scheme unsupported 或 URL 校验失败时不生效。
- `resin-url` 解析失败最多 debug log，不要中断请求。

WebSocket 目标协议映射：

```text
ws  -> http
wss -> https
```

WebSocket 连接 Resin 的 URL scheme 固定使用 `ws`，目标协议仍写入 path 的 `<target-protocol>` 段。不要实现 `http -> wss`、`https -> wss` 或其他推导。

## HTTP Request Mutation

`routingTransport.RoundTrip` 只修改 cloned request，不修改调用者的原 request。生效时：

```go
routedReq.URL = routedURL
routedReq.Host = ""
routedReq.RequestURI = ""
if routedReq.Header == nil {
    routedReq.Header = make(http.Header)
}
deleteHeaderCaseInsensitive(routedReq.Header, "Host")
deleteHeaderCaseInsensitive(routedReq.Header, accountHeader)
routedReq.Header.Set(accountHeader, plan.account)
```

必须清空 `Host`：Antigravity `buildRequest` 会设置 upstream Host，Resin 生效后 TCP/TLS 目标是 Resin，不能继续带原 upstream Host。大小写不同的 Host/account header 变体都必须先删除，避免重复值或绕过覆盖。

未生效时不得修改 request。

## Direct Transport

不要在 Resin 包重复实现默认 transport clone。复用 `sdk/proxyutil.NewDirectTransport()` 创建包级共享 direct transport：

```go
var directTransport http.RoundTripper = proxyutil.NewDirectTransport()
```

Resin 生效时只使用这个共享 direct transport。`http.Transport` 可并发复用，连接池也应复用；关键是 `Proxy=nil`，保证绕过环境 proxy、`auth.ProxyURL` 和 `cfg.ProxyURL`。

这是刻意的网络边界：Resin active path 也不会继承 context RoundTripper 中的自定义 CA、mTLS、DNS、dialer 或 tracing。当前部署要求 CLIProxyAPI 能通过系统 direct transport 访问 `http://resin:2260`。

## RoundTripper Wrapper

`WrapTransport` 是减少 callsite 改动的关键。

行为要求：

- 对 unsupported provider 直接 fallback。
- inactive/unsupported/API-key auth 要返回原 `fallback`，包括 `nil`，不要创建 wrapper。
- 构造时快照 Resin base、platform、account 和 auth policy，不保存 `cfg`/`auth` 指针。
- 对已包过的自有 transport 做幂等保护：相同 plan 可直接返回原 wrapper；不同 plan 先 unwrap 到原 fallback，再创建新 wrapper，不能保留旧 account/config。
- `RoundTrip` 内 clone request，不能改原 request。
- `req.Clone(req.Context())` 已保留 `Body`/`GetBody` 所需语义，不需要读取或复制 body bytes。
- Resin 生效时改写 clone，再使用共享 direct transport。
- Resin 未生效时使用 fallback。
- `fallback == nil` 时等价于标准 `http.Client{Transport:nil}`，即用 `http.DefaultTransport`。
- active path 成功返回 response 后恢复 `resp.Request = req`，调用方不能观察到物理 Resin URL。
- wrapper 实现 `CloseIdleConnections()`，转发到共享 direct transport 和实现该接口的 fallback；同一 transport 不重复调用。

推荐形态：

```go
func (t *routingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    routedReq := req.Clone(req.Context())
    routedURL, ok := routeURL(t.plan, req.URL, false)
    if !ok {
        return fallbackTransport(t.fallback).RoundTrip(req)
    }
    routedReq.URL = routedURL
    routedReq.Host = ""
    routedReq.RequestURI = ""
    if routedReq.Header == nil {
        routedReq.Header = make(http.Header)
    }
    deleteHeaderCaseInsensitive(routedReq.Header, "Host")
    deleteHeaderCaseInsensitive(routedReq.Header, accountHeader)
    routedReq.Header.Set(accountHeader, t.plan.account)

    resp, err := directTransport.RoundTrip(routedReq)
    if resp != nil {
        resp.Request = req
    }
    return resp, err
}
```

## 统一 HTTP 出口

### Raw Helper

在 `internal/runtime/executor/helps/proxy_helpers.go` 新增 raw helper，保留 `main` 原逻辑：

```go
func NewRawProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client
```

raw helper 的优先级保持 `main`：

```text
1. auth.ProxyURL
2. cfg.ProxyURL
3. context roundtripper
4. default transport
```

### Wrapped Helper

原 `NewProxyAwareHTTPClient` 不改签名，只改为 raw 后包 Resin：

```go
func NewProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
    client := NewRawProxyAwareHTTPClient(ctx, cfg, auth, timeout)
    client.Transport = resin.WrapTransport(cfg, auth, client.Transport)
    return client
}
```

wrapped helper 新优先级：

```text
1. Resin direct transport
2. auth.ProxyURL
3. cfg.ProxyURL
4. context roundtripper
5. default transport
```

### uTLS Helper

在 `internal/runtime/executor/helps/utls_client.go` 增加：

```go
func NewRawUtlsHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client
```

`NewRawUtlsHTTPClient` 完整保留当前 uTLS/proxy/context fallback 构造。原 `NewUtlsHTTPClient` 签名不变，只在 raw client 返回前包 Resin：

```go
client := NewRawUtlsHTTPClient(ctx, cfg, auth, timeout)
client.Transport = resin.WrapTransport(cfg, auth, client.Transport)
return client
```

非 Resin 情况下保持 `main`：`api.anthropic.com` 和 `chatgpt.com` 使用 uTLS，其他 host 使用原 standard/context/proxy fallback。

Resin 生效时直接连 Resin，不使用 upstream uTLS transport。

## Plugin HTTP Bridge

`internal/pluginhost/http_bridge.go` 必须改用 raw helper：

```go
client := helps.NewRawProxyAwareHTTPClient(ctx, cfg, c.auth, 0)
```

原因：插件传入的是任意外部 URL，不等于内置账号型 provider runtime upstream。即使 `c.auth.Provider` 是 `codex`、`xai` 或 `claude`，插件 HTTP bridge 也不能把插件请求改写到 Resin。

## SDK 通用 HTTP 请求

`Manager.HttpRequest` 和各原生 executor 的 `HttpRequest` 允许调用方传入任意目标 URL，不属于内置 provider upstream。它们必须保持 raw：

- Codex/Claude `HttpRequest` 使用 `NewRawUtlsHTTPClient`。
- Gemini/Gemini Interactions/Kimi/xAI/Vertex `HttpRequest` 使用 `NewRawProxyAwareHTTPClient`。
- Antigravity `HttpRequest` 使用下面定义的 raw HTTP/1.1 client。

只修改各 executor 的单个 `HttpRequest` 方法；`Execute`、`ExecuteStream`、`CountTokens` 和内置辅助请求继续使用 wrapped helper。这样既不会把任意第三方 URL 改写到 Resin，也不需要给所有正常 runtime callsite 增加参数。

## Antigravity HTTP Client

将 Antigravity client 构造分为 raw 和 wrapped 两层：

```go
func newRawAntigravityHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client
func newAntigravityHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client
```

raw 层保留 HTTP/1.1 fallback，wrapped 层只负责最后包 Resin。

修改原则：

```go
func newRawAntigravityHTTPClient(...) *http.Client {
    client := helps.NewRawProxyAwareHTTPClient(ctx, cfg, auth, timeout)

    // Keep the existing main logic here:
    // - if client.Transport == nil, use shared antigravityTransport
    // - if client.Transport is *http.Transport, cloneTransportWithHTTP11

    return client
}

func newAntigravityHTTPClient(...) *http.Client {
    client := newRawAntigravityHTTPClient(ctx, cfg, auth, timeout)
    client.Transport = resin.WrapTransport(cfg, auth, client.Transport)
    return client
}
```

结果：

- Resin 生效：direct 到 Resin。
- Resin 未生效：保留 Antigravity HTTP/1.1 行为。
- Antigravity 通用 `HttpRequest`：始终使用 raw HTTP/1.1 client，不自动走 Resin。

## WebSocket

HTTP transport 覆盖不到 WebSocket。Codex 和 xAI WebSocket 需要定点接入 Resin。

不要移动 `newProxyAwareWebsocketDialer`。它当前在 `codex_websockets_executor.go`，但已经被 xAI 同包复用；移动文件只会制造无意义 diff。

只新增一个同包 direct dialer helper，可以放在现有文件或一个小的新文件中，但不要做“移动 helper 文件”的整理性改动：

```go
func newDirectWebsocketDialer() *websocket.Dialer
```

direct dialer 要求：

- `Proxy: nil`。
- handshake timeout、compression、`NetDialContext` 默认值与现有 `newProxyAwareWebsocketDialer` 保持一致。

Codex 和 xAI WebSocket 只在 dial 层接入 Resin：

- Codex：只改 `dialCodexWebsocket(ctx, auth, wsURL, headers)` 或其直接下层 helper。
- xAI：只改 `dialXAIWebsocket(ctx, auth, wsURL, headers)` 或其直接下层 helper。
- 不要在 Execute/ExecuteStream 多个分支里散落 route 逻辑。

Dial 层规则：

- request recording 继续记录原始 `wsURL`。
- dial 前解析原始 `wsURL`。
- 调用 `resin.PrepareWebSocket(e.cfg, auth, parsedURL, headers)`。
- route 成功时，`PrepareWebSocket` 返回 `ws://resin:2260/...`、clone 后的 headers、opaque `dialKey` 和 `routed=true`。
- route 成功时，helper 内部必须大小写不敏感地删除所有 `Host` 和旧 `X-Resin-Account` 变体，再设置当前 account；不得修改 caller 的 header map。
- route 成功后使用 returned URL/headers 和 direct dialer。
- route 未成功时使用原始 URL/headers 和 `newProxyAwareWebsocketDialer(e.cfg, auth)`；绝不尝试 `wss` Resin。

`PrepareWebSocket` 必须为 routed 和 fallback 两种模式都返回不可记录日志的 opaque `dialKey`。key 至少绑定：

- actual dial URL 和 route mode。
- Resin base、platform 和 account（routed mode）。
- `auth.ID`。
- `auth.ProxyURL`、`cfg.ProxyURL`（fallback mode）。
- Authorization/Cookie 等影响握手身份的 header digest；digest 只用于内存比较，不能作为 Resin account，也不能记录日志。

Codex/xAI session 增加私有 `dialKey` 字段。`ensureUpstreamConn` 不能再只按 `conn != nil` 无条件复用：

- 新 key 与已连接 session key 相同才复用。
- Resin enable/disable、base/platform/account、upstream URL、auth identity、token 或 fallback proxy 变化时，必须安全脱离并关闭旧连接，然后重新 dial。
- `wsURL` 字段继续保存原始 upstream URL 供日志和错误信息使用，不得改成 Resin URL。
- route/key 计算只放在 `ensureUpstreamConn` 或直接 dial helper，不扩散到 Execute/ExecuteStream 上层。

## Refresh 构造

runtime local refresh 要走 Resin，但首次登录/device flow 尚无 persisted stable account，保持原行为。Home control-plane refresh 继续由 `RefreshAuthViaHome` 提前处理，不经过本实例 Resin。

这是明确的 persisted-account runtime scope，不宣称覆盖 Resin 官方 prompt 中的登录前临时 identity/inherit-lease 语义。

总体规则：

- 保留所有现有 `New*WithProxyURL` 构造函数签名。
- 新增 `New*WithHTTPClient` 构造函数。
- 旧构造继续用于首次 OAuth/device flow。
- executor refresh 使用 Resin-aware client，再传入 `WithHTTPClient`。
- executor 同时传入 `helps.RefreshRouteKey(auth)`；它遵循 `FileName` 优先、`ID` fallback，并加入 auth 包的 singleflight key，防止不同 Resin account 恰好共享 refresh token 时错误复用另一账号的 refresh 请求。
- 构造 HTTP client 时使用 `context.Background()`，保持非 Resin fallback 不读取当前请求 context RoundTripper；真正的 refresh request 仍使用原 `ctx`，取消语义不变。
- `WithHTTPClient(nil)` 必须恢复该 provider 原有默认 client 语义，不能统一替换为 `&http.Client{}`。
- auth 包不需要理解 Resin provider allowlist。

### Codex

`internal/auth/codex/openai_auth.go` 新增：

```go
func NewCodexAuthWithHTTPClient(httpClient *http.Client, refreshRouteKey ...string) *CodexAuth
```

`CodexExecutor.Refresh` 改为：

```go
httpClient := helps.NewProxyAwareHTTPClient(context.Background(), e.cfg, auth, 0)
svc := codexauth.NewCodexAuthWithHTTPClient(httpClient, helps.RefreshRouteKey(auth))
```

Codex token URL 是 `auth.openai.com`，不属于当前 uTLS protected hosts，使用普通 wrapped proxy-aware client 更接近 `main` 语义。

### Claude/Anthropic

`internal/auth/claude/anthropic_auth.go` 新增：

```go
func NewClaudeAuthWithHTTPClient(httpClient *http.Client, refreshRouteKey ...string) *ClaudeAuth
```

`ClaudeExecutor.Refresh` 改为：

```go
httpClient := helps.NewUtlsHTTPClient(context.Background(), e.cfg, auth, 0)
svc := claudeauth.NewClaudeAuthWithHTTPClient(httpClient, helps.RefreshRouteKey(auth))
```

Claude token URL 当前是 `https://api.anthropic.com/v1/oauth/token`，该 host 已在 `NewUtlsHTTPClient` 的 protected hosts 中。Resin 未生效时不能比 `main` 的 TLS 语义更弱。

`NewClaudeAuthWithHTTPClient(nil)` 必须委托现有 Claude 默认 HTTP client 构造，保留 uTLS 行为。

### Kimi

`internal/auth/kimi/kimi.go` 新增：

```go
func NewDeviceFlowClientWithDeviceIDAndHTTPClient(deviceID string, httpClient *http.Client, refreshRouteKey ...string) *DeviceFlowClient
```

`KimiExecutor.Refresh` 改为：

```go
httpClient := helps.NewProxyAwareHTTPClient(context.Background(), e.cfg, auth, 30*time.Second)
client := kimiauth.NewDeviceFlowClientWithDeviceIDAndHTTPClient(resolveKimiDeviceID(auth), httpClient, helps.RefreshRouteKey(auth))
```

`NewDeviceFlowClientWithDeviceIDAndHTTPClient(..., nil)` 必须保留现有 30 秒 credential timeout。

### xAI

`internal/auth/xai/xai.go` 新增：

```go
func NewXAIAuthWithHTTPClient(httpClient *http.Client, refreshRouteKey ...string) *XAIAuth
```

`XAIExecutor.Refresh` 改为：

```go
httpClient := helps.NewProxyAwareHTTPClient(context.Background(), e.cfg, auth, 0)
svc := xaiauth.NewXAIAuthWithHTTPClient(httpClient, helps.RefreshRouteKey(auth))
```

Codex/xAI 的 nil fallback 可以使用其现有 standard client；不能影响旧构造行为。

## 自动覆盖的 Runtime HTTP

因为 `NewProxyAwareHTTPClient` 和 `NewUtlsHTTPClient` 保持签名并在内部包 Resin，本方案不需要逐个 executor 手写正常 runtime request rewrite。只有各 executor 的通用 `HttpRequest` 方法要显式改用 raw helper。

应自动覆盖：

- Codex HTTP runtime，包括 `/responses`、`/responses/compact`、stream 和 Codex OpenAI image 路径。
- Claude HTTP runtime。
- Gemini HTTP runtime，包括 `gemini` 和 `gemini-interactions`。
- Kimi HTTP runtime。
- xAI HTTP runtime。
- xAI video content URL 下载，继续使用 wrapped helper。
- Vertex runtime API 请求。
- Vertex service-account OAuth token minting，因为 `vertexAccessToken` 已通过 `oauth2.HTTPClient` 注入 helper client。
- Antigravity 通过 `newAntigravityHTTPClient` 覆盖的 runtime、refresh、project、credits 请求。
- Antigravity grounding URL HEAD 请求，继续使用 wrapped helper。

不应覆盖：

- AI Studio。
- OpenAI compatibility。
- 任意第三方兼容 provider。
- 任意 `AuthKindAPIKey` auth，包括 provider 名称碰巧在 allowlist 中的 Vertex-compatible/API-key 配置。
- Plugin HTTP bridge。
- SDK `Manager.HttpRequest` 和各 executor 通用 `HttpRequest`。
- Management `api-call`。

Unsupported provider 或 API-key auth 即使调用 wrapped helper，也必须通过 `supportsAuth` fallback 保持 `main` 行为。

## Antigravity Project Discovery

`sdk/auth/filestore.go` 的 `FileTokenStore.List/readAuthFiles` 必须成为纯文件读取路径。删除在缺少 `project_id` 时调用 `FetchAntigravityProjectID(context.Background(), accessToken, http.DefaultClient)` 并回写文件的整个隐式联网分支。

运行时已经有更合适的按需发现和持久化链路，不需要新增 store 依赖：

- `AntigravityExecutor.ShouldPrepareRequestAuth` 检测缺少 `project_id`。
- `PrepareRequestAuth` 使用 `fetchAntigravityProjectID`。
- `fetchAntigravityProjectID` 使用 wrapped `newAntigravityHTTPClient`，因此支持 Resin。
- Manager 的 request-auth prepare 流程会把更新后的 auth 写回 store。

实施时只需确认这条现有链路仍然完整，并补回归测试：单纯 `FileTokenStore.List` 不联网；首次实际 Antigravity runtime 请求按需发现、通过 Resin、更新 manager auth 并持久化 `project_id`。

## 辅助请求覆盖

以下三个路径不经过 executor helper，但本方案必须覆盖，因为它们属于支持 provider 的账号型 runtime 辅助流量。

### `cmd/fetch_codex_models`

该命令必须覆盖 token refresh 和 models fetch。

实现原则：

- 将已加载的 `cfg` 和选中的 `auth` 传入 `ensureAccessToken` 和 `fetchModels`。
- 不引入 `internal/runtime/executor/helps` 依赖。
- 新增命令内本地 helper 创建 Resin-aware client。
- 本地 helper 的 fallback 只保留 `main` 原有 `auth.ProxyURL` 行为，不新增 `cfg.ProxyURL` 或 context roundtripper fallback。
- token refresh 通过 `codexauth.NewCodexAuthWithHTTPClient(httpClient)` 注入 client。
- models fetch 保留 `main` 原 URL、method、headers 和 request body 语义。
- token refresh 和 models fetch 复用同一个本地 helper 创建的 client 即可。

建议形态：

```go
func newCodexModelsHTTPClient(cfg *config.Config, auth *cliproxyauth.Auth) *http.Client {
    client := &http.Client{}
    var fallback http.RoundTripper

    if auth != nil && strings.TrimSpace(auth.ProxyURL) != "" {
        if transport, _, err := proxyutil.BuildHTTPTransport(auth.ProxyURL); err == nil {
            fallback = transport
        }
    }

    client.Transport = resin.WrapTransport(cfg, auth, fallback)
    return client
}
```

Resin 生效时必须 direct 到 Resin；Resin 未生效时必须保持 `main` 的 `auth.ProxyURL` fallback。

### `cmd/fetch_antigravity_models`

该命令的 project discovery 和 models fetch 都必须在本方案中覆盖。

实现原则：

- 将已加载的 `cfg` 和选中的 `auth` 传入 `fetchModels`。
- 不引入 `internal/runtime/executor/helps` 依赖。
- 新增命令内本地 helper 创建 Resin-aware client。
- 本地 helper 的 fallback 只保留 `main` 原有 `auth.ProxyURL` 行为，不新增 `cfg.ProxyURL` 或 context roundtripper fallback。
- 保留 `http.Client{Timeout: 30 * time.Second}`。
- 如果选中的 auth 缺少 `project_id`，先用同一个 Resin-aware client 调用 `sdk/auth.FetchAntigravityProjectID`。
- 发现成功后更新 `auth.Metadata["project_id"]` 并通过当前 `FileTokenStore.Save` 持久化；失败时返回明确错误，不恢复 `FileTokenStore.List` 的隐式联网。
- models fetch 保留 `main` 原 URL、method、headers、request body 和 base URL retry 语义。

建议形态：

```go
func newAntigravityModelsHTTPClient(cfg *config.Config, auth *cliproxyauth.Auth) *http.Client {
    client := &http.Client{Timeout: 30 * time.Second}
    var fallback http.RoundTripper

    if auth != nil && strings.TrimSpace(auth.ProxyURL) != "" {
        if transport, _, err := proxyutil.BuildHTTPTransport(auth.ProxyURL); err == nil {
            fallback = transport
        }
    }

    client.Transport = resin.WrapTransport(cfg, auth, fallback)
    return client
}
```

Resin 生效时 project discovery 和 models fetch 都必须 direct 到 Resin；Resin 未生效时必须保持 `main` 的 `auth.ProxyURL` fallback 和 30s timeout。

### `sdk/cliproxy/antigravity_models.go`

该文件的 model hint fetch 必须在本方案中覆盖。

实现原则：

- 保留现有 `antigravityModelFetchProxyURL(auth)` fallback 逻辑。
- 按现有逻辑用 `proxyutil.BuildHTTPTransport(...)` 构造 fallback transport。
- 构造完 fallback 后，用 `resin.WrapTransport(s.cfg, auth, fallback)` 包一层。
- 不把 Resin 下沉到 `sdk/proxyutil`。

Resin 生效时必须 direct 到 Resin；Resin 未生效时必须保持现有 `auth.ProxyURL` > `s.cfg.ProxyURL` fallback。

## Management api-call

`internal/api/handlers/management/api_tools.go` 本方案不改。

必须保持：

- `$TOKEN$` 替换。
- 用户 headers。
- 用户 Host override。
- `http.Client{Timeout: defaultAPICallTimeout}`。
- `httpClient.Transport = h.apiCallTransport(auth)`。
- `apiCallTransport` 的原 proxy/direct fallback 语义。

禁止：

- 不调用 `resin.WrapTransport`。
- 不清理 management `api-call` 的 Host override。
- 不把 selected auth 的 provider allowlist 应用于 management `api-call`。
- 不让 management `api-call` 的 Antigravity token refresh 自动走 Resin。

## Logging

已有 request recording 继续记录原始 upstream URL，不记录 Resin URL。

实现顺序：

- 先执行已有 `RecordAPIRequest` 或 `RecordAPIWebsocketRequest`。
- 发送阶段由 `WrapTransport` 或 WebSocket route helper 改写 cloned request/实际 dial URL。
- `X-Resin-Account` 只来自 `FileName` 或 `ID` 原值；允许包含本地文件名、email 或路径信息，因为 Resin 是受信任的自有服务。
- WebSocket `dialKey`、Authorization/Cookie digest 和物理 Resin URL 不记录日志。

## 测试策略

测试要收敛，不需要每个 executor 每个方法都写完整 e2e。

必须覆盖：

- `internal/resin` 私有 route、stable account、`supportsAuth`、HTTP request mutation、header、Host cleanup、共享 direct transport。
- Resin V1 path 使用裸 platform identity segment 和 `X-Resin-Account`；不产生 `LEGACY_V0` 冒号格式。
- 配置示例 `http://resin:2260/ThePasswd` + `cpa` 生成预期 route。
- HTTP Resin base 校验：`http`/`https`、非空 Host；拒绝 userinfo/query/fragment/Opaque。
- WebSocket Resin base 只接受 `http -> ws`；`https` 不路由且不尝试 `wss`。
- escaped-path：`%2F`、`%25`、空格、Unicode、重复斜杠、dot segments、trailing slash 和 escaped base path 不被清理或二次编码。
- target `Host` 保留端口和 IPv6 方括号；`RawQuery` 顺序和重复 key 保持。
- account 使用 `FileName` 优先、`ID` fallback，不做 hash；非法 header value fallback。
- API-key auth negative test，至少覆盖 `Provider: "vertex"` 的 Vertex-compatible 配置。
- helper wrapped path：支持 provider + 完整配置 + stable account 请求到达 Resin test server。
- helper fallback path：配置缺失、account 缺失、unsupported provider、API-key auth 时保持原 proxy/context/default 行为。
- Resin 生效时不访问 `auth.ProxyURL` 或 `cfg.ProxyURL`。
- 原 request 的 URL、RawPath、Header、Host、RequestURI、Body/GetBody 不被修改。
- active response 的 `resp.Request` 恢复为原逻辑 request。
- `CloseIdleConnections` 能到达 direct/fallback transport。
- `NewUtlsHTTPClient` Resin 生效时 direct 到 Resin，未生效时保留 uTLS fallback。
- plugin HTTP bridge 使用 raw helper，不访问 Resin。
- SDK/各 executor 通用 `HttpRequest` 使用 raw helper，不访问 Resin。
- Antigravity wrapped client：Resin 生效 direct 到 Resin，未生效保留 HTTP/1.1 fallback；通用 `HttpRequest` 使用 raw HTTP/1.1 client。
- Codex/xAI WebSocket：route、header clone、case-insensitive Host cleanup、account header、direct dialer。
- Codex/xAI WebSocket session：相同 `dialKey` 复用；Resin enable/disable、base/platform/account、upstream URL、auth/token、proxy 变化时重拨。
- Gemini runtime 通过 wrapped helper 走 Resin，包括 `gemini` 和 `gemini-interactions`。
- Codex/Claude/Kimi/xAI refresh 使用 `WithHTTPClient` 注入的 client。
- `WithHTTPClient(nil)` 保留每个 provider 的原默认 client 语义，尤其是 Claude uTLS 和 Kimi 30s timeout。
- Vertex service-account token minting 通过 `oauth2.HTTPClient` 走 wrapped helper。
- `FileTokenStore.List` 不执行 Antigravity 网络请求。
- Antigravity runtime 缺少 `project_id` 时通过 Resin 按需发现并由 Manager 持久化。
- `cmd/fetch_codex_models` token refresh 和 models fetch 走 Resin。
- `cmd/fetch_antigravity_models` project discovery 和 models fetch 走 Resin，并保存发现的 `project_id`。
- `sdk/cliproxy/antigravity_models.go` model hint fetch 走 Resin。
- Management `api-call` 在完整 Resin 配置和 allowlist auth 下仍不访问 Resin。

Unsupported provider 至少覆盖：

- `aistudio`。
- `openai-compatibility` 或第三方兼容 provider。
- `anthropic`。
- allowlist provider + `AuthKindAPIKey`。

不要再补“每个 provider 每个 runtime 方法”的完整 e2e。HTTP provider 大部分经由 helper 自动覆盖，测试重点放在 helper、例外路径和 negative case。

## 实施顺序

1. 添加 `SDKConfig.ResinURL`、`SDKConfig.ResinPlatformName` 和 `config.example.yaml` 示例。
2. 新增 `internal/resin`，只导出 `WrapTransport`、`PrepareWebSocket`，完成 V1 route/escaping/auth policy 核心单测。
3. 删除 `FileTokenStore.List/readAuthFiles` 的 Antigravity 隐式联网和回写分支，补纯读取回归测试。
4. 在 `helps/proxy_helpers.go` 新增 `NewRawProxyAwareHTTPClient`，并让原 helper 在 raw transport 外包 Resin。
5. 在 `helps/utls_client.go` 新增 `NewRawUtlsHTTPClient`，并让原 helper 在 raw transport 外包 Resin。
6. 将 plugin bridge 和各 executor 通用 `HttpRequest` 改用对应 raw helper。
7. 将 Antigravity client 拆成 raw HTTP/1.1 factory 和 wrapped factory。
8. 给 Codex/Claude/Kimi/xAI auth helper 增加保留 provider 默认 nil 语义的 `WithHTTPClient` 构造。
9. 修改 Codex/Claude/Kimi/xAI executor local refresh 使用 `context.Background()` 创建 Resin-aware client 并注入。
10. 只在 Codex/xAI WebSocket dial/`ensureUpstreamConn` 层接入 `PrepareWebSocket`、direct dialer 和 `dialKey` 比较，不移动 helper 文件。
11. 修改 `cmd/fetch_codex_models`：新增命令内本地 client helper，覆盖 token refresh 和 models fetch。
12. 修改 `cmd/fetch_antigravity_models`：新增命令内本地 client helper，覆盖 project discovery、持久化和 models fetch。
13. 修改 `sdk/cliproxy/antigravity_models.go` 的 model hint fetch。
14. 确认 Antigravity runtime request-auth prepare 通过 wrapped client 发现并持久化 `project_id`。
15. 补 helper、negative、WebSocket reuse、辅助入口和 management exclusion 测试。
16. 运行格式化、测试和 build。

## main 同步与长期维护

更新频繁时，必须把分支职责固定下来：

- 本地 `main`：由维护者手动同步外部更新，不放 Resin 私有提交。
- 本地 `develop`：在 `main` 之上保留 Resin 实现和其他本地开发提交。
- `origin/main`、`origin/develop`：个人远端上的对应分支；只在本地验证完成后推送。
- `sync/main-*`：一次同步使用的临时集成分支，用于解决冲突和运行测试，避免直接污染 `develop`。

本流程不配置或使用额外的源码 remote。开始集成前，由维护者负责完成本地 `main` 的手动同步并确认目标 commit 正确；后续流程只在本地 `main` 与 `develop` 之间工作。

本章假设 Resin 实现已经提交在 `develop`。工作区存在未提交或未跟踪的实现文件时，不得开始同步。尤其不能让 `resin.md`、`internal/resin/` 或新增测试以未跟踪状态跨分支移动。

### 每次同步前的硬性检查

先确认 `develop` 工作区完全干净：

```bash
git switch develop
git status --short --branch
git diff --check
```

必须满足：

- 没有未提交修改。
- 没有未跟踪的 Resin 源码、测试或文档。
- 当前 `develop` 已通过至少一轮 Resin 定向测试和 server build。
- 本地重要提交已经存在于分支历史中，而不是只存在于 stash。

不推荐依赖 stash 承载 Resin 实现后再同步。stash 容易被遗忘，也会把冲突推迟到更难判断的阶段。确实需要保留未完成工作时，应先放到独立 WIP 分支并提交，再返回干净的 `develop`。

同步前查看分支关系：

```bash
git log --oneline --decorate --graph --max-count=30 --all
git log --oneline --left-right main...develop
```

### 确认本地 main 已手动同步

维护者完成手动同步后，先确认本地 `main` 工作区干净并记录目标 commit：

```bash
git switch main
git status --short --branch
git rev-parse main
git log --oneline --decorate -10
```

禁止把 Resin 私有提交带入 `main`。如果 `main` 的目标 commit、历史来源或工作区状态不明确，应停止同步并由维护者先完成确认；不要用 `git reset --hard`、强制 checkout 或 force push 掩盖问题。

本地 `main` 确认无误后，可以选择同步个人远端：

```bash
git push origin main
```

这一步不是合并 `develop` 的前置条件。不得使用 `--force`。

### 先记录更新后 main 的测试基线

每次合并前，应在手动同步后的本地 `main` 上运行基线验证。这样可以区分“`main` 本来就失败”和“Resin 合并引入回归”：

```bash
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./...
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go build -o test-output ./cmd/server && rm -f test-output
```

不要在 `main` 上运行会改写工作树的批量格式化或代码生成命令。基线阶段只验证已确认的 `main` commit，不制造本地差异。

记录以下信息：

- `git rev-parse main` 的 commit。
- `go test ./...` 的通过、跳过和失败数量。
- 每个失败的测试名称和错误位置。
- server build 是否成功。

不能永久沿用本文当前记录的既有失败。`main` 可能修复、删除或改变该测试；每次同步都以更新后的 `main` 实测结果为准。

### 使用临时集成分支合并

不要直接在 `develop` 上开始一次可能产生大量冲突的 merge。推荐在同一个 Bash 会话中执行：

```bash
git switch develop
sync_branch="sync/main-$(date +%Y%m%d-%H%M%S)"
git switch -c "$sync_branch"
git merge --no-commit --no-ff main
```

`--no-commit` 让 merge 在提交前停下，便于审计、格式化和测试；`--no-ff` 保留明确的 `main` 同步边界。此时 `develop` 仍指向同步前的安全提交。

如果输出为 `Already up to date`，不需要创建 merge commit。确认后返回 `develop` 并删除空的临时分支即可。

如果 merge 产生冲突，先查看冲突集合：

```bash
git status --short
git diff --name-only --diff-filter=U
git diff --cc
```

需要放弃本次未提交 merge 时使用：

```bash
git merge --abort
git switch develop
```

不要使用 `git reset --hard` 清理冲突。

### Resin 冲突解决原则

冲突解决的目标不是机械保留旧代码，而是把 Resin 能力重新接到 `main` 最新、正确的出口位置。必须先理解 `main` 变更，再做最小适配。

通用原则：

- 不要对关键冲突文件整体使用 `ours` 或 `theirs`。
- 优先保留 `main` 新增的错误处理、认证字段、请求头、重试、流式处理和连接生命周期语义。
- Resin 逻辑继续集中在 `internal/resin`，不要因为冲突在 executor、SDK 或 command 中复制 route 构造代码。
- `main` 删除或替换旧 helper 时，把 Resin 包装迁移到新的 canonical 出口，不要保留已经失效的旧 helper 形成双路径。
- 所有请求必须重新判断是 wrapped runtime 流量还是明确的 raw 旁路，不能仅以“原文件以前怎么处理”为依据。
- 不增加与 `main` 架构无关的兼容层；只有真实持久化数据、已发布 API 或明确外部调用方需要时才保留兼容代码。

高冲突文件和必须保持的语义：

- `internal/config/sdk_config.go`、`config.example.yaml`：保留 `resin-url`、`resin-platform-name`，同时吸收 `main` 新增配置字段和注释顺序。
- `internal/resin/`：继续作为唯一 V1 route 实现；保持 escaped path、API-key 排除、stable account、direct transport、response restoration 和 WS header clone 语义。
- `internal/runtime/executor/helps/proxy_helpers.go`：raw helper 保持原 proxy/context/default 优先级，wrapped helper只在最外层调用 `resin.WrapTransport`。
- `internal/runtime/executor/helps/utls_client.go`：先完整保留 `main` 最新 uTLS/protected-host fallback，再在最外层包 Resin。
- `internal/pluginhost/http_bridge.go`：继续使用 raw helper，不能因 `main` 统一 client factory 而意外进入 Resin。
- 各 executor 的 `HttpRequest`：继续 raw；正常 runtime、token mint、grounding、credits、project 等账号流量继续 wrapped。
- `internal/runtime/executor/antigravity_executor.go`：保持 raw HTTP/1.1 client 与 wrapped client 分层，不能重新把 project discovery 放回文件枚举阶段。
- `sdk/auth/filestore.go`：`List/readAuthFiles` 必须保持纯读取，不允许恢复 Antigravity 隐式联网和回写。
- Codex/xAI WebSocket 文件：在最终 dial 前调用 `PrepareWebSocket`；routed 使用 direct dialer；session 复用继续比较覆盖 URL、account、headers 和 proxy 的 `dialKey`。
- Codex/Claude/Kimi/xAI auth 与 executor：保留可注入 HTTP client、provider 原 nil 默认语义，以及包含 route account 的 refresh singleflight key。
- `cmd/fetch_codex_models`、`cmd/fetch_antigravity_models`、`sdk/cliproxy/antigravity_models.go`：继续显式包装 Resin，同时保留各入口原 proxy 优先级和 credential timeout。
- Management `api-call`、plugin bridge、SDK generic `HttpRequest`：继续 raw，不得因为调用了新公共 helper而自动路由到 Resin。

如果 `main` 新增 provider，不要直接加入 Resin allowlist。只有同时满足以下条件才考虑加入：

- provider 是内置账号型 runtime，而不是 API-key 或任意兼容 BaseURL。
- auth 有稳定的 persisted `FileName` 或 `ID`。
- runtime、refresh、WebSocket 和辅助请求的出口边界已经识别清楚。
- generic `HttpRequest`、management 和 plugin 等旁路不会被误覆盖。
- 已补 positive、API-key negative 和 raw bypass 测试。

### 提交 merge 前的结构审计

冲突解决后，先检查 merge 相对同步前 `develop` 的净变化：

```bash
git diff --name-status HEAD
git diff --stat HEAD
git diff --check HEAD
```

重点搜索 Resin 接入点是否仍唯一且完整：

```bash
git grep -n "ResinURL\|ResinPlatformName"
git grep -n "WrapTransport\|PrepareWebSocket"
git grep -n "NewRawProxyAwareHTTPClient\|NewRawUtlsHTTPClient"
git grep -n "RefreshRouteKey"
git grep -n "FetchAntigravityProjectID" -- sdk/auth internal/runtime cmd sdk/cliproxy
```

检查结果必须满足：

- route 构造没有散落到 `internal/resin` 之外。
- wrapped helper 没有递归包装或重复包装。
- raw 旁路没有调用 wrapped helper。
- `main` 新增的 HTTP/WS 出口没有绕开既有 Resin policy。
- `FileTokenStore.List` 没有恢复网络副作用。
- 没有修改 `internal/translator/` 来承载 Resin。
- 没有新增日志输出 Resin account、Authorization、Cookie、dialKey 或物理 Resin URL。

### 分层回归验证

先执行快速 Resin 回归：

```bash
gofmt -w .
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/auth/codex ./internal/auth/claude ./internal/auth/kimi ./internal/auth/xai
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/runtime/executor/helps
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/runtime/executor -run 'Resin|Websocket|Refresh|Antigravity|Vertex|Gemini|Kimi|XAI|Codex|Claude'
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/pluginhost ./internal/api/handlers/management ./sdk/auth ./sdk/cliproxy ./cmd/fetch_codex_models ./cmd/fetch_antigravity_models -run 'Resin|FileTokenStore|Antigravity|GenericHTTP|APICall'
```

定向测试通过后，再执行完整验证：

```bash
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./...
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go build -o test-output ./cmd/server && rm -f test-output
git diff --check
```

结果判断规则：

- 更新后的 `main` 通过、集成分支失败：视为 Resin 合并回归，必须修复后才能完成 merge。
- `main` 和集成分支在同一测试以相同原因失败：记录为 `main` 基线问题，但仍检查 Resin 是否扩大影响。
- `main` 原有失败在集成分支消失：确认不是测试被误删、跳过或条件被改变。
- 测试数量明显减少：检查 `main` 是否移动 package、删除测试，或 merge 是否意外丢失 Resin 测试。
- build 失败或 `git diff --check` 失败：不得提交 merge。

如果 `main` 改动涉及 HTTP transport、OAuth、WebSocket、auth store 或 provider 注册，还应执行真实环境 smoke test：

- eligible persisted OAuth auth 的 HTTP runtime 请求命中 Resin V1 path，并携带预期 account。
- Codex/xAI WebSocket 连接命中 `ws://resin...`，配置或账号变化后会重拨。
- API-key、第三方 compatibility、management `api-call`、plugin bridge 和 generic `HttpRequest` 不命中 Resin。
- Antigravity 缺少 `project_id` 时经 Resin 发现并持久化，重启后不重复发现。
- Resin disabled 时原 proxy/context/default 和 uTLS 行为保持不变。

### 完成集成并更新 develop

所有审计和测试通过后，在临时集成分支完成 merge commit：

先对每个明确解决、格式化或适配过的文件执行 `git add <path>`，不要批量暂存整个工作树。然后执行：

```bash
sync_branch=$(git branch --show-current)
git status --short
git diff
git diff --cached
git diff --check
git diff --cached --check
git diff --cached --stat
git commit -m "merge: sync main into develop"
```

只对明确解决、格式化或适配过的文件执行 `git add`。merge 自动暂存的 `main` 文件仍需通过 `git diff --cached` 审查。提交前必须确认没有把 auth 文件、token、`.env`、构建产物、日志或 `.trellis/` 加入索引。

然后让 `develop` fast-forward 到已验证的集成结果：

```bash
git switch develop
git merge --ff-only "$sync_branch"
git branch -d "$sync_branch"
git status --short --branch
git show --no-patch --pretty=%P HEAD
```

最后按需推送个人远端：

```bash
git push origin develop
```

不得 force push。推送前再次确认 `develop` 的 merge commit 包含两个正确 parent：同步前的 `develop` 和更新后的 `main`。

### 同步失败与恢复

在 merge commit 创建前失败：

- 使用 `git merge --abort` 返回临时分支的 merge 前状态。
- 切回 `develop` 即可恢复稳定开发分支。
- 保留临时分支用于调查，确认不再需要后再删除。
- 不需要也不应使用 `git reset --hard`。

merge commit 创建后但尚未 fast-forward `develop`：

- 保持 `develop` 不动。
- 在临时分支继续追加修复提交，重新运行验证。
- 确认完全放弃时，切回 `develop`；临时分支可保留为问题现场。

已经 fast-forward 或推送 `develop` 后发现问题：

- 优先在 `develop` 上做最小 fix-forward，不改写已共享历史。
- 确实必须撤销整个 merge 时，使用新的 revert commit，并明确记录 mainline parent；不要 force push。
- revert merge 会影响 Git 对后续相同 `main` 提交的合并判断，下一次同步前必须先评估是否需要 revert 该 revert。

### 每次同步完成后的记录

至少在 merge commit、PR 描述或维护日志中记录：

- 合入的 `main` commit SHA。
- 是否发生冲突以及冲突文件。
- Resin 接入点是否因 `main` 架构变化而迁移。
- 更新后 `main` 的测试基线。
- 集成分支的定向测试、全仓测试和 build 结果。
- 仍存在的 `main` 基线失败及其文件/行号。
- 是否完成真实 Resin smoke test。

`resin.md` 的最终检查表应反映当前 `develop` 的真实状态。`main` 改变测试数量、修复既有失败或引入新出口后，应更新对应条目，不能长期保留过期数字和结论。

### 每次同步速查表

- [ ] `develop` 工作区干净，Resin 实现和文档均已提交。
- [ ] 只配置所需的个人远端，不依赖额外源码 remote。
- [ ] 本地 `main` 已由维护者手动同步并确认目标 commit，没有 Resin 私有提交。
- [ ] 已记录更新后 `main` 的 commit、全仓测试和 build 基线。
- [ ] 已从 `develop` 创建独立 `sync/main-*` 临时集成分支。
- [ ] 使用 `git merge --no-commit --no-ff main`，未直接污染 `develop`。
- [ ] 所有冲突都按 `main` 最新架构重新判断 wrapped/raw 边界，没有整体选用 `ours`/`theirs`。
- [ ] `internal/resin` 仍是唯一 route implementation，HTTP/WS/refresh/project discovery 接入点完整。
- [ ] API-key、compatibility、management、plugin 和 generic `HttpRequest` 旁路仍然 raw。
- [ ] `FileTokenStore.List` 仍无网络副作用。
- [ ] Resin 定向测试、四个 auth refresh 测试、executor/WS 测试和辅助入口测试通过。
- [ ] 已对比集成结果与同一 `main` commit 的全仓测试基线。
- [ ] server build 和 `git diff --check` 通过。
- [ ] `main` 高风险改动已执行真实 Resin smoke test。
- [ ] merge commit 只包含审查过的文件，不包含 secret、运行数据、构建产物或 `.trellis/`。
- [ ] `develop` 只通过 `git merge --ff-only "$sync_branch"` 接收已验证结果。
- [ ] 推送未使用 force，merge commit 的两个 parent 正确。
- [ ] 已更新测试数字、已知失败和维护记录，删除临时集成分支。

## 验证命令

本地网络不能直连 `github.com` 等 Go module 来源，所有 `go test` / `go build` 验证命令都必须通过本机代理执行：

```bash
export HTTP_PROXY=http://127.0.0.1:1081
export HTTPS_PROXY=http://127.0.0.1:1081
export ALL_PROXY=http://127.0.0.1:1081
export NO_PROXY=localhost,127.0.0.1,::1
```

这些代理变量只用于本地验证命令，不写入项目配置、文档示例配置或 CI 脚本。

Go 代码改完后必须执行：

```bash
gofmt -w .
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./...
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go build -o test-output ./cmd/server && rm -f test-output
```

开发中可先跑：

```bash
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/runtime/executor/helps
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/runtime/executor -run 'Resin|Websocket|Vertex|Antigravity|Claude|Gemini|Kimi|XAI|Codex'
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/pluginhost -run Resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./internal/api/handlers/management -run Resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./sdk/auth -run 'FileTokenStore|Antigravity'
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./cmd/fetch_codex_models -run Resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./cmd/fetch_antigravity_models -run Resin
HTTP_PROXY=http://127.0.0.1:1081 HTTPS_PROXY=http://127.0.0.1:1081 ALL_PROXY=http://127.0.0.1:1081 NO_PROXY=localhost,127.0.0.1,::1 go test ./sdk/cliproxy -run Resin
```

## 最终检查表

### 范围与配置

- [x] `SDKConfig` 已新增 `ResinURL` 和 `ResinPlatformName`，YAML/JSON tag 分别为 `resin-url` 和 `resin-platform-name`。
- [x] `config.example.yaml` 已在 `proxy-url` 附近新增两个空值示例。
- [x] 实际部署配置可使用 `resin-url: "http://resin:2260/ThePasswd"` 和 `resin-platform-name: "cpa"`。
- [x] 未把 Resin 塞入 `ProxyURL`，未修改 `sdk/proxyutil.BuildHTTPTransport`、`BuildDialer` 或 `internal/util/proxy.go` 语义。
- [x] 未修改 `internal/translator/`、README、management UI、CI 或 watcher diff。
- [x] 未新增 management 专用 Resin CRUD；完整 `/config` 和 `/config.yaml` 自然包含 Resin 字段。
- [x] 实现范围明确为 persisted-account runtime，不包含首次 OAuth/device flow 临时 identity/inherit-lease。
- [x] Home 模式下 Resin 配置继续由远端完整配置决定。

### Core API 与 Auth Policy

- [x] `internal/resin` 是唯一 canonical Resin implementation。
- [x] `internal/resin` 只导出 `WrapTransport` 和 `PrepareWebSocket`。
- [x] `X-Resin-Account` 常量保持包内私有，caller 不自行注入 Resin header。
- [x] 未新增 `WrapClient`、public `DirectTransport`、public `RouteHTTP`、public `ApplyHTTP` 或 account-only wrapper。
- [x] `WrapTransport` 和 `PrepareWebSocket` 构造 immutable route plan，不长期持有 `cfg`/`auth` 指针。
- [x] `supportsAuth` 只允许 `codex`、`claude`、`antigravity`、`gemini`、`gemini-interactions`、`kimi`、`xai`、`vertex`。
- [x] `anthropic`、`aistudio`、OpenAI compatibility 和其他第三方 provider 不在 allowlist。
- [x] `auth.AuthKind() == AuthKindAPIKey` 时始终 fallback，即使 provider 名称在 allowlist 中。
- [x] `stableAccount` 按 `auth.FileName` 优先、`auth.ID` fallback，不做 SHA/hash、basename 或匿名化转换。
- [x] account 允许保留本地文件名、email 或绝对路径，但拒绝空值、CR/LF、控制字符和超长 header value。
- [x] 不从 metadata、`AccountInfo()`、API key、token、cookie 或 provider subject 另行派生 account。

### Resin V1 Route

- [x] 只实现最新版 Resin V1，不生成 `LEGACY_V0` 的 `<platform>:` identity。
- [x] HTTP Resin base 只接受 hierarchical `http`/`https`、非空 `Host`，拒绝 userinfo/query/fragment/Opaque。
- [x] WebSocket Resin base 只接受 `http` 并映射为 client-to-Resin `ws`；未实现或尝试 `wss`。
- [x] `resin-platform-name` 是单个 V1 identity segment，并拒绝 `/`、`.`、`:`、控制字符和歧义值。
- [x] `http://resin:2260/ThePasswd` + `cpa` + HTTPS upstream 生成 `/ThePasswd/cpa/https/<host>/<path>`。
- [x] WebSocket target protocol 按 `ws -> http`、`wss -> https` 写入 route path。
- [x] route 使用 `target.Host`，保留端口和 IPv6 方括号，不使用 `Hostname()`。
- [x] route 只基于 `base.EscapedPath()` 和 `target.EscapedPath()`，未使用 `path.Join`、`url.JoinPath` 或 decoded `Path` 拼接。
- [x] routed URL 的 `Path`/`RawPath` 一致，保留 `%2F`、`%25`、percent-escape 大小写、重复斜杠、dot segments 和 trailing slash。
- [x] 目标 `RawQuery` 顺序、重复 key 和 `ForceQuery` 已复制；fragment 不发送，target Opaque/空 Host fallback。

### HTTP Transport

- [x] private shared direct transport 由 `proxyutil.NewDirectTransport()` 创建，`Proxy=nil`，不为每个 request 重建。
- [x] inactive/unsupported/API-key auth 时 `WrapTransport` 原样返回 fallback，包括 `nil`。
- [x] 相同 route plan 重复包装不增加层数；不同 plan 会 unwrap 旧 wrapper 并使用新 account/config。
- [x] active path clone request，不修改原 URL、RawPath、Header、Host、RequestURI、Body、GetBody 或 Trailer。
- [x] active path 清空 clone 的 `Host` 和 `RequestURI`，并大小写不敏感地删除 Host/account header 旧值。
- [x] nil request Header 已安全初始化，再设置单一 `X-Resin-Account`。
- [x] active path 只用 shared direct transport，不访问 auth/global/environment proxy 或 context RoundTripper。
- [x] inactive path 保持原 fallback transport 和 `http.DefaultTransport` 语义。
- [x] active response 的 `resp.Request` 已恢复为原逻辑 upstream request。
- [x] wrapper 实现 `CloseIdleConnections` 并正确转发到 direct/fallback transport。

### HTTP Helper 与旁路

- [x] `NewProxyAwareHTTPClient` 签名未变，并在 `NewRawProxyAwareHTTPClient` 的 transport 外包 Resin。
- [x] `NewRawProxyAwareHTTPClient` 保留 `auth.ProxyURL > cfg.ProxyURL > context roundtripper > default` 优先级。
- [x] `NewUtlsHTTPClient` 签名未变，并在 `NewRawUtlsHTTPClient` 的 transport 外包 Resin。
- [x] Resin 未生效时 uTLS protected hosts 和原 standard/context/proxy fallback 保持不变。
- [x] Plugin HTTP bridge 使用 `NewRawProxyAwareHTTPClient`，不访问 Resin。
- [x] SDK `Manager.HttpRequest` 对应的各 executor `HttpRequest` 使用 raw helper，不改写任意目标 URL。
- [x] Codex/Claude 通用 `HttpRequest` 使用 raw uTLS helper。
- [x] Gemini/Gemini Interactions/Kimi/xAI/Vertex 通用 `HttpRequest` 使用 raw proxy-aware helper。
- [x] Antigravity 已拆分 raw HTTP/1.1 client 和 wrapped client；通用 `HttpRequest` 只用 raw client。
- [x] 没有给所有正常 HTTP runtime callsite 增加 provider 参数。

### WebSocket

- [x] Codex/xAI 只在现有 dial/`ensureUpstreamConn` 层接入 Resin，未移动共享 dialer 文件。
- [x] direct WebSocket dialer 使用 `Proxy:nil`，其余 handshake/compression/dial 参数与原 helper 一致。
- [x] `PrepareWebSocket` routed path 返回 `ws://resin...`、cloned headers、opaque `dialKey` 和 `routed=true`。
- [x] routed headers 大小写不敏感地删除 Host/account旧值，保留 Authorization、Cookie、OpenAI-Beta 等其他 headers。
- [x] `PrepareWebSocket` 不修改 caller URL 或 header map。
- [x] fallback path 使用原 upstream URL/headers 和原 proxy-aware dialer，不尝试 Resin `wss`。
- [x] Codex/xAI session 已新增私有 `dialKey`，不再因 `conn != nil` 无条件复用。
- [x] 相同 `dialKey` 复用连接；Resin enable/disable、base/platform/account、upstream URL、auth/token、fallback proxy 变化时关闭并重拨。
- [x] session `wsURL` 和 request recording 继续使用原 upstream URL。
- [x] `dialKey`、Authorization/Cookie digest 和物理 Resin URL 不记录日志。

### Refresh

- [x] Codex/Claude/Kimi/xAI auth helper 已新增 `WithHTTPClient` 构造，旧 `WithProxyURL`/首次登录构造签名未变。
- [x] executor local refresh 使用 `context.Background()` 创建 Resin-aware client，并继续把原 `ctx` 传给实际 refresh request。
- [x] Codex refresh 使用普通 wrapped proxy-aware client，不误用 uTLS helper。
- [x] Claude refresh 使用 wrapped uTLS client。
- [x] Kimi refresh 保留 30 秒 credential timeout。
- [x] xAI refresh 使用 wrapped proxy-aware client。
- [x] `WithHTTPClient(nil)` 保留各 provider 原默认语义，尤其是 Claude uTLS 和 Kimi 30s timeout。
- [x] refresh singleflight key 包含稳定 route account，不能跨不同 Resin account 合并同一 refresh token 的请求。
- [x] `RefreshAuthViaHome` 仍在 local refresh 前返回，不经过 Resin。

### Antigravity Project 与辅助请求

- [x] `FileTokenStore.List/readAuthFiles` 已删除 Antigravity project discovery 网络副作用和隐式文件回写。
- [x] 单纯列举 auth 文件不会访问 Antigravity upstream、Resin 或 proxy。
- [x] Antigravity runtime 缺少 `project_id` 时，现有 request-auth prepare 使用 wrapped client 经 Resin 按需发现。
- [x] runtime 发现的 `project_id` 会更新 Manager auth 并通过现有 store 持久化。
- [x] Antigravity wrapped client Resin active 时 direct 到 Resin，inactive 时保留 HTTP/1.1 fallback。
- [x] `cmd/fetch_codex_models` token refresh 和 models fetch 走 Resin，inactive fallback 仍只使用 `auth.ProxyURL`。
- [x] `cmd/fetch_antigravity_models` 缺少 `project_id` 时先通过同一 Resin-aware client 发现并 `FileTokenStore.Save`。
- [x] `cmd/fetch_antigravity_models` models fetch 走 Resin，inactive fallback 保留 `auth.ProxyURL` 和独立的 30s models timeout。
- [x] `sdk/cliproxy/antigravity_models.go` model hint fetch 走 Resin，inactive fallback 保留 `auth.ProxyURL > cfg.ProxyURL`。

### Runtime 覆盖与排除

- [x] Codex、Claude、Gemini、Gemini Interactions、Kimi、xAI、Vertex 和 Antigravity 内置 runtime HTTP 经 wrapped helper 覆盖。
- [x] xAI video content、Antigravity grounding、credits、refresh、project 请求经 wrapped helper覆盖。
- [x] Vertex service-account token mint 通过 `oauth2.HTTPClient` 使用 wrapped helper。
- [x] API-key/第三方兼容 auth 即使 provider 名称命中 allowlist 也不访问 Resin。
- [x] Management `api-call` 未调用 `resin.WrapTransport`，保留用户 Host override、timeout 和原 proxy/direct 语义。
- [x] Management `api-call` 的 Antigravity token refresh 未自动接入 Resin。

### 测试与验证

- [x] `internal/resin` 已覆盖 V1 route、base validation、auth policy、account、escaped path、query、Host/header 和 response restoration。
- [x] 已覆盖 HTTP active/fallback、proxy bypass、request immutability、redirect/body replay 和 `CloseIdleConnections`。
- [x] 已覆盖 `vertex` API-key、`anthropic`、`aistudio`、OpenAI compatibility 和第三方 provider negative cases。
- [x] 已覆盖 Codex/xAI WebSocket header clone、Host cleanup、direct dial 和 `dialKey` reuse/reconnect。
- [x] 已覆盖 raw helper、plugin bridge、SDK 通用 `HttpRequest` 和 management `api-call` 不访问 Resin。
- [x] 已覆盖四个 provider refresh 注入、route-account singleflight isolation 及 nil default client 语义。
- [x] 已覆盖 `FileTokenStore.List` 无网络副作用和 Antigravity runtime/command project discovery 持久化。
- [x] 已覆盖两个 fetch command 和 SDK Antigravity model hint。
- [x] `gofmt -w .` 已执行。
- [x] 定向 package tests 已通过。
- [ ] `go test ./...` 已通过。当前结果为 3124 passed、3 skipped、1 个既有失败：`internal/api/server_test.go:683` 的 priority 实际为 143、期望为 129。
- [x] `go build -o test-output ./cmd/server && rm -f test-output` 已通过。
