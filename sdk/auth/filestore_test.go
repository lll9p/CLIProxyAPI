package auth

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestFileTokenStoreListDoesNotDiscoverMissingAntigravityProjectID(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "antigravity.json")
	original := []byte(`{
  "type": "antigravity",
  "access_token": "access-token",
  "email": "user@example.com",
  "label": "Antigravity account",
  "disabled": false
}`)
	if errWrite := os.WriteFile(path, original, 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}

	requestCount := 0
	originalDefaultClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: fileStoreRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"cloudaicompanionProject":"discovered-project"}`)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() {
		http.DefaultClient = originalDefaultClient
	})

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auths, errList := store.List(context.Background())
	if errList != nil {
		t.Fatalf("List() error = %v", errList)
	}
	if requestCount != 0 {
		t.Fatalf("List() made %d HTTP requests, want none", requestCount)
	}

	afterList, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read auth file: %v", errRead)
	}
	if !reflect.DeepEqual(afterList, original) {
		t.Fatalf("auth file changed during List():\ngot:  %s\nwant: %s", afterList, original)
	}

	if len(auths) != 1 {
		t.Fatalf("List() len = %d, want one auth", len(auths))
	}
	auth := auths[0]
	if auth.ID != "antigravity.json" || auth.FileName != "antigravity.json" || auth.Provider != "antigravity" {
		t.Fatalf("auth identity = ID %q, FileName %q, Provider %q", auth.ID, auth.FileName, auth.Provider)
	}
	if auth.Label != "Antigravity account" || auth.Status != cliproxyauth.StatusActive || auth.Disabled {
		t.Fatalf("auth metadata fields = Label %q, Status %q, Disabled %v", auth.Label, auth.Status, auth.Disabled)
	}
	wantMetadata := map[string]any{
		"type":         "antigravity",
		"access_token": "access-token",
		"email":        "user@example.com",
		"label":        "Antigravity account",
		"disabled":     false,
	}
	if !reflect.DeepEqual(auth.Metadata, wantMetadata) {
		t.Fatalf("auth Metadata = %#v, want %#v", auth.Metadata, wantMetadata)
	}
	if _, exists := auth.Metadata["project_id"]; exists {
		t.Fatalf("auth Metadata project_id = %#v, want absent", auth.Metadata["project_id"])
	}
	if auth.Attributes[cliproxyauth.AttributePath] != path || auth.Attributes[cliproxyauth.AttributeSource] != path {
		t.Fatalf("auth source attributes = %#v, want path %q", auth.Attributes, path)
	}
	if auth.Attributes[cliproxyauth.AttributeSourceBackend] != cliproxyauth.AuthSourceFile {
		t.Fatalf("auth source backend = %q, want %q", auth.Attributes[cliproxyauth.AttributeSourceBackend], cliproxyauth.AuthSourceFile)
	}
	if auth.Attributes["email"] != "user@example.com" {
		t.Fatalf("auth email attribute = %q, want user@example.com", auth.Attributes["email"])
	}
}

func TestFileTokenStoreListExpandsPluginMultiAuths(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "geminicli.json")
	if errWrite := os.WriteFile(path, []byte(`{"type":"gemini-cli","headers":{"X-Test":"value"}}`), 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}

	RegisterPluginAuthParser(fileStoreMultiAuthParserFunc(func(ctx context.Context, req pluginapi.AuthParseRequest) ([]*cliproxyauth.Auth, bool, error) {
		if req.Provider != "gemini-cli" || req.Path != path || req.FileName != "geminicli.json" {
			t.Fatalf("ParseAuths request = %#v, want file context", req)
		}
		return []*cliproxyauth.Auth{
			{
				ID:       "geminicli.json",
				Provider: "gemini-cli",
				Metadata: map[string]any{
					"type": "gemini-cli",
					"headers": map[string]any{
						"X-Test": "value",
					},
				},
			},
			nil,
			{
				ID:       "geminicli-project-a.json",
				Provider: "gemini-cli",
				Metadata: map[string]any{
					"type":       "gemini-cli",
					"project_id": "project-a",
					"headers": map[string]any{
						"X-Test": "value",
					},
				},
			},
		}, true, nil
	}))
	t.Cleanup(func() {
		RegisterPluginAuthParser(nil)
	})

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auths, errList := store.List(context.Background())
	if errList != nil {
		t.Fatalf("List() error = %v", errList)
	}
	if len(auths) != 2 {
		t.Fatalf("List() len = %d, want two plugin auths", len(auths))
	}
	if firstIndex, secondIndex := auths[0].EnsureIndex(), auths[1].EnsureIndex(); firstIndex == "" || firstIndex == secondIndex {
		t.Fatalf("auth indexes = %q/%q, want distinct non-empty indexes", firstIndex, secondIndex)
	}
	for _, auth := range auths {
		if !cliproxyauth.IsPluginVirtualAuth(auth) {
			t.Fatalf("auth attributes = %#v, want plugin virtual marker", auth.Attributes)
		}
		if auth.Attributes[cliproxyauth.AttributeVirtualSource] != path {
			t.Fatalf("virtual_source = %q, want %q", auth.Attributes[cliproxyauth.AttributeVirtualSource], path)
		}
		if auth.Attributes["path"] != path || auth.Attributes["source"] != path {
			t.Fatalf("auth attributes = %#v, want source path", auth.Attributes)
		}
		if gotHeader := auth.Attributes["header:X-Test"]; gotHeader != "value" {
			t.Fatalf("header:X-Test = %q, want value", gotHeader)
		}
	}
	if gotProject := auths[1].Metadata["project_id"]; gotProject != "project-a" {
		t.Fatalf("project_id = %#v, want project-a", gotProject)
	}
}

func TestFileTokenStoreListAppliesSourceDisabledToPluginMultiAuths(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "geminicli.json")
	if errWrite := os.WriteFile(path, []byte(`{"type":"gemini-cli","disabled":true}`), 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}

	RegisterPluginAuthParser(fileStoreMultiAuthParserFunc(func(context.Context, pluginapi.AuthParseRequest) ([]*cliproxyauth.Auth, bool, error) {
		return []*cliproxyauth.Auth{
			{ID: "geminicli.json", Provider: "gemini-cli", Metadata: map[string]any{"type": "gemini-cli"}},
			{ID: "geminicli-project-a.json", Provider: "gemini-cli", Metadata: map[string]any{"type": "gemini-cli", "project_id": "project-a"}},
		}, true, nil
	}))
	t.Cleanup(func() {
		RegisterPluginAuthParser(nil)
	})

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auths, errList := store.List(context.Background())
	if errList != nil {
		t.Fatalf("List() error = %v", errList)
	}
	if len(auths) != 2 {
		t.Fatalf("List() len = %d, want two plugin auths", len(auths))
	}
	for _, auth := range auths {
		if !auth.Disabled || auth.Status != cliproxyauth.StatusDisabled {
			t.Fatalf("auth %s disabled/status = %v/%s, want disabled", auth.ID, auth.Disabled, auth.Status)
		}
		if got, _ := auth.Metadata["disabled"].(bool); !got {
			t.Fatalf("auth %s metadata disabled = %#v, want true", auth.ID, auth.Metadata["disabled"])
		}
	}
}

func TestFileTokenStoreListPluginHandledEmptySuppressesBuiltin(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "codex.json")
	if errWrite := os.WriteFile(path, []byte(`{"type":"codex","access_token":"token"}`), 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}

	RegisterPluginAuthParser(fileStoreMultiAuthParserFunc(func(context.Context, pluginapi.AuthParseRequest) ([]*cliproxyauth.Auth, bool, error) {
		return nil, true, nil
	}))
	t.Cleanup(func() {
		RegisterPluginAuthParser(nil)
	})

	store := NewFileTokenStore()
	store.SetBaseDir(baseDir)
	auths, errList := store.List(context.Background())
	if errList != nil {
		t.Fatalf("List() error = %v", errList)
	}
	if len(auths) != 0 {
		t.Fatalf("List() len = %d, want plugin-handled empty result", len(auths))
	}
}

type fileStoreMultiAuthParserFunc func(context.Context, pluginapi.AuthParseRequest) ([]*cliproxyauth.Auth, bool, error)

type fileStoreRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f fileStoreRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (f fileStoreMultiAuthParserFunc) ParseAuth(context.Context, pluginapi.AuthParseRequest) (*cliproxyauth.Auth, bool, error) {
	return nil, false, nil
}

func (f fileStoreMultiAuthParserFunc) ParseAuths(ctx context.Context, req pluginapi.AuthParseRequest) ([]*cliproxyauth.Auth, bool, error) {
	return f(ctx, req)
}
