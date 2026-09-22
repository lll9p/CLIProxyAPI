package config

import (
	"strings"
	"testing"
)

func TestResinV8ConfigMigration(t *testing.T) {
	const legacy = "resin-url: http://old-resin:2260/base\nresin-platform-name: cpa\n"
	const current = "config-version: 8\nrequests:\n  resin-url: http://new-resin:2260/base\n  resin-platform-name: new-platform\n"
	for _, test := range []struct {
		name, input, wantURL, wantPlatform string
	}{
		{"legacy", legacy, "http://old-resin:2260/base", "cpa"},
		{"v8", current, "http://new-resin:2260/base", "new-platform"},
		{"v8 precedence", legacy + current, "http://new-resin:2260/base", "new-platform"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := ParseConfigBytes([]byte(test.input))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ResinURL != test.wantURL || cfg.ResinPlatformName != test.wantPlatform {
				t.Fatalf("Resin config = %q, %q; want %q, %q", cfg.ResinURL, cfg.ResinPlatformName, test.wantURL, test.wantPlatform)
			}
		})
	}
	if err := ValidateV8Config([]byte(current)); err != nil {
		t.Fatalf("validate v8 Resin config: %v", err)
	}
	migrated, changed, err := NormalizeConfigLayout([]byte(legacy), true)
	if err != nil || !changed || !strings.Contains(string(migrated), "  resin-url: http://old-resin:2260/base") || !strings.Contains(string(migrated), "  resin-platform-name: cpa") {
		t.Fatalf("migrated Resin config = %q, changed = %t, error = %v", migrated, changed, err)
	}
	if err := ValidateV8Config(migrated); err != nil {
		t.Fatalf("validate migrated Resin config: %v", err)
	}
}
