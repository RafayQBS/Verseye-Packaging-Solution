package versioning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type VersionRecord struct {
	Version      int      `json:"version"`
	FullTag      string   `json:"full_tag"`
	ShortHash    string   `json:"short_hash"`
	InputHash    string   `json:"input_hash"`
	Dependencies []string `json:"dependencies"`
	BuildCommand string   `json:"build_command"`
}

func GetVersionsPath(feature string) string {
	return filepath.Join(".builder-cache", feature, "versions.json")
}

func LoadVersions(feature string) ([]VersionRecord, error) {
	path := GetVersionsPath(feature)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []VersionRecord{}, nil
		}
		return nil, err
	}

	var versions []VersionRecord
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func AppendVersionRecord(feature string, record VersionRecord) (VersionRecord, error) {
	versions, err := LoadVersions(feature)
	if err != nil {
		return record, err
	}

	record.Version = len(versions) + 1
	versions = append(versions, record)

	data, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return record, err
	}

	path := GetVersionsPath(feature)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return record, err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return record, err
	}

	return record, os.Rename(tmpPath, path)
}
