package thumbnail

import (
	"strings"
	"testing"
)

func TestToken_RoundTripAndTamper(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	const orderID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const variantID = "11111111-1111-4111-8111-111111111111"
	token := Token(key, orderID, variantID)
	if strings.Contains(token, "=") || strings.Contains(token, "+") || strings.Contains(token, "/") {
		t.Fatalf("token %q is not raw base64url", token)
	}
	gotOrder, gotVariant, ok := Verify(key, token)
	if !ok || gotOrder != orderID || gotVariant != variantID {
		t.Fatalf("verify %q %q ok=%v", gotOrder, gotVariant, ok)
	}
	other := Token(key, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", variantID)
	payload, _, _ := strings.Cut(other, ".")
	_, mac, _ := strings.Cut(token, ".")
	if _, _, ok := Verify(key, payload+"."+mac); ok {
		t.Fatal("changed order id was accepted")
	}
	if _, _, ok := Verify(key, token[:len(token)-4]); ok {
		t.Fatal("truncated token was accepted")
	}
	if _, _, ok := Verify(key, "not-a-token"); ok {
		t.Fatal("random token was accepted")
	}
	if _, _, ok := Verify("ffffffffffffffffffffffffffffffff", token); ok {
		t.Fatal("other key was accepted")
	}
}
