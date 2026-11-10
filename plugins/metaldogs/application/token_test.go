package application_test

import (
	"testing"

	"github.com/akarso/shopanda/plugins/metaldogs/application"
)

func TestMintOwnerToken_HashNotPlaintext(t *testing.T) {
	t.Parallel()
	tok, hash, err := application.MintOwnerToken()
	if err != nil || tok == "" || len(hash) != 32 {
		t.Fatalf("tok=%q hashLen=%d err=%v", tok, len(hash), err)
	}
	if application.OwnerTokenMatches(tok, hash) != true {
		t.Fatal("match")
	}
	if application.OwnerTokenMatches(tok+"x", hash) {
		t.Fatal("mismatch should fail")
	}
	got, err := application.HashOwnerToken(tok)
	if err != nil || len(got) != 32 {
		t.Fatal(err)
	}
	for i := range hash {
		if hash[i] != got[i] {
			t.Fatal("hash mismatch")
		}
	}
}
