// Package imagemime sniffs image Content-Types from magic bytes only.
package imagemime

import "bytes"

const (
	JPEG = "image/jpeg"
	PNG  = "image/png"
	WEBP = "image/webp"
)

// Sniff returns image/jpeg, image/png, or image/webp from magic bytes.
// Extension and declared Content-Type are never consulted. Empty string = unknown.
func Sniff(photo []byte) string {
	if len(photo) >= 8 && bytes.Equal(photo[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) {
		return PNG
	}
	if len(photo) >= 3 && photo[0] == 0xff && photo[1] == 0xd8 && photo[2] == 0xff {
		return JPEG
	}
	if len(photo) >= 12 && string(photo[0:4]) == "RIFF" && string(photo[8:12]) == "WEBP" {
		return WEBP
	}
	return ""
}
