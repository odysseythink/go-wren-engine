package difftest

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// CompareOptions tunes structural JSON equality. Defaults are strict.
type CompareOptions struct {
	// LooseNodeLocation: when comparing an object whose path ends in
	// "nodeLocation", allow (line, column) to differ by ±1 (P5 risk #15:
	// deep-identifier token positions drift between ANTLR Go and ANTLR Java).
	LooseNodeLocation bool
}

func JSONEqual(a, b []byte) error {
	return JSONEqualWithOptions(a, b, CompareOptions{})
}

func JSONEqualWithOptions(a, b []byte, opts CompareOptions) error {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return fmt.Errorf("unmarshal a: %w", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return fmt.Errorf("unmarshal b: %w", err)
	}
	return jsonValueEqual(av, bv, "", opts)
}

func jsonValueEqual(a, b any, path string, opts CompareOptions) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return fmt.Errorf("%s: %v != %v", path, a, b)
	}
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return fmt.Errorf("%s: type mismatch (number vs %T)", path, b)
		}
		if opts.LooseNodeLocation && strings.HasSuffix(path, ".line") || strings.HasSuffix(path, ".column") {
			if math.Abs(av-bv) <= 1 {
				return nil
			}
		}
		if av != bv {
			return fmt.Errorf("%s: %v != %v", path, av, bv)
		}
	case string:
		bv, ok := b.(string)
		if !ok || av != bv {
			return fmt.Errorf("%s: %q != %v", path, av, b)
		}
	case bool:
		bv, ok := b.(bool)
		if !ok || av != bv {
			return fmt.Errorf("%s: %v != %v", path, av, b)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return fmt.Errorf("%s: type mismatch (array vs %T)", path, b)
		}
		if len(av) != len(bv) {
			return fmt.Errorf("%s: array length %d != %d", path, len(av), len(bv))
		}
		for i := range av {
			if err := jsonValueEqual(av[i], bv[i], fmt.Sprintf("%s[%d]", path, i), opts); err != nil {
				return err
			}
		}
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: type mismatch (object vs %T)", path, b)
		}
		if len(av) != len(bv) {
			// surface which keys differ for debuggability
			var aKeys, bKeys []string
			for k := range av {
				aKeys = append(aKeys, k)
			}
			for k := range bv {
				bKeys = append(bKeys, k)
			}
			return fmt.Errorf("%s: key count %d != %d (a:%v b:%v)", path, len(av), len(bv), aKeys, bKeys)
		}
		for k, va := range av {
			vb, ok := bv[k]
			if !ok {
				return fmt.Errorf("%s: key %q missing in b", path, k)
			}
			if err := jsonValueEqual(va, vb, fmt.Sprintf("%s.%s", path, k), opts); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Errorf("%s: %v != %v", path, a, b)
		}
	}
	return nil
}
