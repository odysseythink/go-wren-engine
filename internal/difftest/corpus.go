package difftest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case is one differential testing case: an MDL manifest plus a SQL query.
type Case struct {
	Group        string
	Name         string
	ManifestJSON []byte
	SQL          string
	ModelingOnly bool
}

// ID returns the case's logical identifier "<group>/<name>".
func (c Case) ID() string { return c.Group + "/" + c.Name }

type groupConfig struct {
	ModelingOnly bool `json:"modelingOnly"`
}

// LoadCorpus loads every case under root. Layout: root/<group>/{mdl.json,
// group.json, queries/<name>.sql}. Cases are returned sorted by ID.
func LoadCorpus(root string) ([]Case, error) {
	groups, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read corpus root %s: %w", root, err)
	}
	var cases []Case
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		groupDir := filepath.Join(root, g.Name())

		manifest, err := os.ReadFile(filepath.Join(groupDir, "mdl.json"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}

		cfgRaw, err := os.ReadFile(filepath.Join(groupDir, "group.json"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}
		var cfg groupConfig
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("group %s group.json: %w", g.Name(), err)
		}

		queries, err := os.ReadDir(filepath.Join(groupDir, "queries"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}
		for _, q := range queries {
			if q.IsDir() || !strings.HasSuffix(q.Name(), ".sql") {
				continue
			}
			sql, err := os.ReadFile(filepath.Join(groupDir, "queries", q.Name()))
			if err != nil {
				return nil, fmt.Errorf("group %s query %s: %w", g.Name(), q.Name(), err)
			}
			cases = append(cases, Case{
				Group:        g.Name(),
				Name:         strings.TrimSuffix(q.Name(), ".sql"),
				ManifestJSON: manifest,
				SQL:          string(sql),
				ModelingOnly: cfg.ModelingOnly,
			})
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID() < cases[j].ID() })
	return cases, nil
}
