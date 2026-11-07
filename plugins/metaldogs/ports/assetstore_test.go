package ports_test

import (
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

func TestObjectKey(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC)
	got, err := ports.ObjectKey("source", "asset-1", "jpg", at)
	if err != nil {
		t.Fatal(err)
	}
	want := "private/blobs/metaldogs/source/2026/11/asset-1.jpg"
	if got != want {
		t.Fatalf("ObjectKey = %q, want %q", got, want)
	}
}

func TestObjectKey_RejectsEmptyAndUnsafe(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC()
	cases := []struct {
		typ, id, ext string
	}{
		{"", "a", "jpg"},
		{"source", "", "jpg"},
		{"source", "a", ""},
		{"a/b", "id", "jpg"},
		{"source", "../x", "jpg"},
		{"source", "id", "jp g"},
	}
	for _, tc := range cases {
		if _, err := ports.ObjectKey(tc.typ, tc.id, tc.ext, at); err == nil {
			t.Fatalf("ObjectKey(%q,%q,%q) want error", tc.typ, tc.id, tc.ext)
		}
	}
}

func TestObjectKey_NormalizesExtDot(t *testing.T) {
	t.Parallel()
	got, err := ports.ObjectKey("preview", "x1", ".png", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "/x1.png") || !strings.Contains(got, "/2026/01/") {
		t.Fatalf("got %q", got)
	}
}
