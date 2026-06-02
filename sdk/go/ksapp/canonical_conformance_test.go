package ksapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	kstypes "github.com/wuhanyuhan/ks-types"
)

func TestCanonicalDerivationConformance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "shared-fixtures", "canonical_derivation.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var f struct {
		Cases []struct {
			AppID     string `json:"app_id"`
			Name      string `json:"name"`
			Canonical string `json:"canonical"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, c := range f.Cases {
		if got := kstypes.Canonical(c.AppID, c.Name); got != c.Canonical {
			t.Fatalf("Canonical(%q,%q)=%q want %q", c.AppID, c.Name, got, c.Canonical)
		}
	}
}
