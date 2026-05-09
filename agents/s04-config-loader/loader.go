package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Load layers configuration from four sources, lowest precedence first:
//
//  1. DefaultConfig()       — the in-binary baseline
//  2. ~/.config/claude-omc/config.json  (user-wide)
//  3. <workingDir>/.claude/omc.json     (per-project)
//  4. environment variables  (OMC_MODEL_*, OMC_DISABLE_TOOLS)
//
// Missing files are skipped silently — a fresh user has none of them and
// should still get a working Config. **Malformed** JSON is a hard error;
// silently swallowing parse failures hides typos in user config and is
// the most common confused-deputy footgun in TS-side configurators.
//
// Note we use `encoding/json` and not a JSONC parser. Upstream supports
// JSONC for the `.jsonc` files; the curriculum drops it (plan §"Risks #2
// — we drop JSONC") to keep the dependency surface stdlib-only. If a user
// genuinely wants comments, README docs are a better place for them than
// production config files.
func Load(workingDir string) (Config, error) {
	cfg := DefaultConfig()
	dstMap, err := configToMap(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("encode defaults: %w", err)
	}

	for _, src := range configFilePaths(workingDir) {
		layer, err := readLayer(src)
		if err != nil {
			return Config{}, err
		}
		if layer == nil {
			continue
		}
		dstMap = deepMerge(dstMap, layer)
	}

	merged, err := mapToConfig(dstMap)
	if err != nil {
		return Config{}, fmt.Errorf("decode merged config: %w", err)
	}
	applyEnvOverlay(&merged)
	return merged, nil
}

// configFilePaths returns the user → project ordering used by Load. The
// user file is consulted first so the project file always wins on
// conflict — matches upstream `src/config/loader.ts` L4–L9 commentary.
func configFilePaths(workingDir string) []string {
	paths := make([]string, 0, 2)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "claude-omc", "config.json"))
	}
	if workingDir != "" {
		paths = append(paths, filepath.Join(workingDir, ".claude", "omc.json"))
	}
	return paths
}

// readLayer reads a single overlay file. Returns (nil, nil) when the file
// does not exist; (nil, err) on parse failure. Empty / whitespace-only
// files round-trip to an empty map — that is a no-op overlay, not an
// error.
func readLayer(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

// configToMap and mapToConfig bridge between the typed Config struct and
// the dynamic `map[string]any` representation deepMerge operates on. The
// detour keeps the merge generic (any sub-shape works) while the public
// API stays typed.
func configToMap(cfg Config) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mapToConfig(m map[string]any) (Config, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
