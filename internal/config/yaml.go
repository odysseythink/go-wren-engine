package config

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// parseYAML reads a YAML file and flattens nested maps into dot-notation keys.
// Supported scalar types: string, int, int64, float64, bool, nil.
func parseYAML(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}

	result := make(map[string]string)
	flattenYAML("", root, result)
	return result, nil
}

func flattenYAML(prefix string, src map[string]interface{}, dst map[string]string) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flattenYAML(key, val, dst)
		case string:
			dst[key] = val
		case int:
			dst[key] = strconv.Itoa(val)
		case int64:
			dst[key] = strconv.FormatInt(val, 10)
		case float64:
			dst[key] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			dst[key] = strconv.FormatBool(val)
		case nil:
			dst[key] = ""
		default:
			dst[key] = fmt.Sprintf("%v", val)
		}
	}
}

// writeYAML writes a flat map[string]string as YAML in alphabetic key order.
// All values are emitted as YAML strings so that "false", "123", etc. round-trip safely.
func writeYAML(w io.Writer, props map[string]string) error {
	root := &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: make([]*yaml.Node, 0, len(props)*2),
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: props[k]},
		)
	}

	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	defer encoder.Close()
	return encoder.Encode(root)
}
