package main

import "testing"

func TestLogBufferListAndClear(t *testing.T) {
	buf := newLogBuffer(2)
	if _, err := buf.Write([]byte("first\nsecond\nthird\n")); err != nil {
		t.Fatal(err)
	}
	got := buf.list(10, "")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Message != "second" || got[1].Message != "third" {
		t.Fatalf("unexpected entries: %+v", got)
	}
	filtered := buf.list(10, "third")
	if len(filtered) != 1 || filtered[0].Message != "third" {
		t.Fatalf("unexpected filtered entries: %+v", filtered)
	}
	buf.clear()
	if entries := buf.list(10, ""); len(entries) != 0 {
		t.Fatalf("entries after clear = %d, want 0", len(entries))
	}
}
