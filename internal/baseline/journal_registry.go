package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReadJournalRegistry 核验独立版本化期刊种子清单；来源历史资格与当下复核严格区分。
func ReadJournalRegistry(directory string) ([]Venue, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "registry.json"))
	if err != nil {
		return nil, err
	}
	manifestRaw, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Version      int    `json:"version"`
		Hash         string `json:"registrySHA256"`
		Entries      int    `json:"entries"`
		CoverageHash string `json:"coverageSHA256"`
	}
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	if manifest.Version != 1 || hex.EncodeToString(hash[:]) != manifest.Hash {
		return nil, fmt.Errorf("journal registry checksum mismatch")
	}
	if manifest.CoverageHash != "" {
		coverage, err := os.ReadFile(filepath.Join(directory, "coverage.json"))
		if err != nil {
			return nil, err
		}
		coverageHash := sha256.Sum256(coverage)
		if hex.EncodeToString(coverageHash[:]) != manifest.CoverageHash {
			return nil, fmt.Errorf("journal registry coverage checksum mismatch")
		}
	}
	var entries []Venue
	if err = json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	if len(entries) != manifest.Entries {
		return nil, fmt.Errorf("journal registry count mismatch")
	}
	seen := map[string]bool{}
	for _, v := range entries {
		key := v.Name + ":" + v.RegistryVersion
		if v.Name == "" || v.RegistryVersion == "" || seen[key] {
			return nil, fmt.Errorf("invalid journal registry identity")
		}
		seen[key] = true
	}
	return entries, nil
}
