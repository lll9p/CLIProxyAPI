package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResinV8Config(t *testing.T) {
	const current = "config-version: 8\nrequests:\n  resin-url: http://resin:2260/base\n  resin-platform-name: cpa\n"
	cfg, err := ParseConfigBytes([]byte(current))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResinURL != "http://resin:2260/base" || cfg.ResinPlatformName != "cpa" {
		t.Fatalf("Resin config = %q, %q", cfg.ResinURL, cfg.ResinPlatformName)
	}
	if err := ValidateV8Config([]byte(current)); err != nil {
		t.Fatalf("validate v8 Resin config: %v", err)
	}
	snapshot, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConfigBytes(snapshot); err != nil {
		t.Fatalf("parse Resin snapshot: %v", err)
	}
	var snapshotNode yaml.Node
	if err := yaml.Unmarshal(snapshot, &snapshotNode); err != nil {
		t.Fatal(err)
	}
	if yamlPath(snapshotNode.Content[0], "resin-url") != nil || yamlPath(snapshotNode.Content[0], "resin-platform-name") != nil {
		t.Fatalf("snapshot contains legacy Resin fields: %s", snapshot)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfigPreserveComments(path, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ResinURL != cfg.ResinURL || reloaded.ResinPlatformName != cfg.ResinPlatformName {
		t.Fatalf("saved Resin config = %q, %q", reloaded.ResinURL, reloaded.ResinPlatformName)
	}
	if err := os.WriteFile(path, []byte("port: 8317\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfigPreserveComments(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConfigBytes(saved); err != nil {
		t.Fatalf("parse saved mixed-layout config: %v", err)
	}
}

func TestResinLegacyLayoutRejected(t *testing.T) {
	for _, key := range []string{"resin-url", "resin-platform-name"} {
		t.Run(key, func(t *testing.T) {
			raw := []byte(key + ": old-value\n")
			if _, err := ParseConfigBytes(raw); err == nil || !strings.Contains(err.Error(), "requests."+key) {
				t.Fatalf("ParseConfigBytes() error = %v, want requests.%s hint", err, key)
			}
			if _, _, err := NormalizeConfigLayout(raw, true); err == nil || !strings.Contains(err.Error(), "requests."+key) {
				t.Fatalf("NormalizeConfigLayout() error = %v, want requests.%s hint", err, key)
			}
			if err := ValidateV8Config(raw); err == nil || !strings.Contains(err.Error(), "requests."+key) {
				t.Fatalf("ValidateV8Config() error = %v, want requests.%s hint", err, key)
			}
		})
	}
}
