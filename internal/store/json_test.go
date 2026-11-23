package store

import "testing"

func TestMustJSON(t *testing.T) {
	if string(mustJSON(nil)) != "{}" {
		t.Fatal("nil")
	}
	b := mustJSON(map[string]any{"a": 1})
	if string(b) != `{"a":1}` {
		t.Fatalf("got %s", b)
	}
}
