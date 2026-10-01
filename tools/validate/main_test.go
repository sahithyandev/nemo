package main

import (
	"bytes"
	"testing"
)

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"-manifest", "missing"}, {"-unknown"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("code=%d", code)
		}
	}
	var out, err bytes.Buffer
	if code := run([]string{"-h"}, &out, &err); code != 0 {
		t.Fatalf("help=%d", code)
	}
}
