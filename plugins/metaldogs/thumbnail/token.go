package thumbnail

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const tokenMACLabel = "order-thumbnail:"

// Token is base64url(orderID|variantID).base64url(HMAC-SHA256(key, "order-thumbnail:"+orderID+"|"+variantID)).
// It does not expire. key is the raw signing secret.
func Token(key, orderID, variantID string) string {
	payload := orderID + "|" + variantID
	return b64([]byte(payload)) + "." + b64(macSum(key, tokenMACLabel+payload))
}

// Verify checks token with HMAC in constant time (hmac.Equal).
// A bad or truncated token returns ok false and empty ids.
func Verify(key, token string) (orderID, variantID string, ok bool) {
	payload, sig, wellFormed := tokenParts(token)
	orderID, variantID, pair := splitPair(payload)
	msg := tokenMACLabel
	if pair {
		msg = tokenMACLabel + orderID + "|" + variantID
	}
	expected := macSum(key, msg)
	if sig == nil {
		sig = []byte{}
	}
	if !hmac.Equal(sig, expected) || !wellFormed || !pair {
		return "", "", false
	}
	return orderID, variantID, true
}

func tokenParts(token string) (payload string, sig []byte, ok bool) {
	left, right, found := strings.Cut(token, ".")
	if !found || left == "" || right == "" || strings.Contains(right, ".") {
		return "", nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(left)
	if err != nil {
		return "", nil, false
	}
	sig, err = base64.RawURLEncoding.DecodeString(right)
	if err != nil {
		return "", nil, false
	}
	return string(raw), sig, true
}

func splitPair(payload string) (orderID, variantID string, ok bool) {
	orderID, variantID, found := strings.Cut(payload, "|")
	if !found || orderID == "" || variantID == "" || strings.Contains(variantID, "|") {
		return "", "", false
	}
	return orderID, variantID, true
}

func macSum(key, msg string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(msg))
	return h.Sum(nil)
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
