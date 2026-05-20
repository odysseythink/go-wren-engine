package difftest

import "testing"

func TestJSONCompare(t *testing.T) {
	a := []byte(`{"a":1,"b":[1,2,{"c":true}]}`)
	b := []byte(`{"b":[1,2,{"c":true}],"a":1}`)
	if err := JSONEqual(a, b); err != nil {
		t.Fatalf("expected equal: %v", err)
	}
	c := []byte(`{"a":1}`)
	if err := JSONEqual(a, c); err == nil {
		t.Fatal("expected diff")
	}
}
