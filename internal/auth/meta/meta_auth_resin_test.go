package meta

import (
	"net/http"
	"testing"
)

func TestNewMetaAuthWithHTTPClient(t *testing.T) {
	injected := &http.Client{}
	if got := NewMetaAuthWithHTTPClient(nil, injected).httpClient; got != injected {
		t.Fatal("injected HTTP client was not preserved")
	}
	if got := NewMetaAuthWithHTTPClient(nil, nil).httpClient.Timeout; got != httpClientTimeout {
		t.Fatalf("default timeout = %v, want %v", got, httpClientTimeout)
	}
}
