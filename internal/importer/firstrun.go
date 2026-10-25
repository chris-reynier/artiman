package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chris-reynier/artiman/internal/store"
)

// SeedCompany applies data/import/settings.json (company identity and other settings) and installs
// data/import/logo.png, once. These files come from a private folder at build time, so the repository
// never holds the business identity. Each file is renamed with an ".importe" suffix once applied.
func SeedCompany(s *store.Store, importDir, dataDir string) (bool, error) {
	applied := false
	sp := filepath.Join(importDir, "settings.json")
	if b, err := os.ReadFile(sp); err == nil {
		var values map[string]string
		if err := json.Unmarshal(b, &values); err != nil {
			return false, fmt.Errorf("settings.json illisible : %w", err)
		}
		if err := s.SaveSettings(values); err != nil {
			return false, err
		}
		os.Rename(sp, sp+".importe")
		applied = true
	}
	lp := filepath.Join(importDir, "logo.png")
	if _, err := os.Stat(lp); err == nil {
		dst := filepath.Join(dataDir, "logo.png")
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			b, err := os.ReadFile(lp)
			if err != nil {
				return applied, err
			}
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return applied, err
			}
		}
		os.Rename(lp, lp+".importe")
		applied = true
	}
	return applied, nil
}
