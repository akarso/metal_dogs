package imageprep

import (
	"encoding/binary"
)

// jpegOrientation returns EXIF Orientation (1–8) or 1 when missing/unreadable.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xff {
			return 1
		}
		marker := data[i+1]
		i += 2
		// Standalone markers without length.
		if marker == 0xd8 || marker == 0xd9 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		if i+2 > len(data) {
			return 1
		}
		seglen := int(binary.BigEndian.Uint16(data[i:]))
		if seglen < 2 || i+seglen > len(data) {
			return 1
		}
		payload := data[i+2 : i+seglen]
		i += seglen
		if marker != 0xe1 {
			continue
		}
		if orient, ok := parseExifOrientation(payload); ok {
			return orient
		}
	}
	return 1
}

func parseExifOrientation(app1 []byte) (int, bool) {
	if len(app1) < 14 || string(app1[:6]) != "Exif\x00\x00" {
		return 0, false
	}
	tiff := app1[6:]
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 0, false
	}
	ifdOff := int(bo.Uint32(tiff[4:8]))
	if ifdOff < 0 || ifdOff+2 > len(tiff) {
		return 0, false
	}
	n := int(bo.Uint16(tiff[ifdOff:]))
	entryBase := ifdOff + 2
	for e := 0; e < n; e++ {
		off := entryBase + e*12
		if off+12 > len(tiff) {
			return 0, false
		}
		tag := bo.Uint16(tiff[off:])
		typ := bo.Uint16(tiff[off+2:])
		count := bo.Uint32(tiff[off+4:])
		if tag != 0x0112 || typ != 3 || count != 1 {
			continue
		}
		val := int(bo.Uint16(tiff[off+8:]))
		if val >= 1 && val <= 8 {
			return val, true
		}
		return 0, false
	}
	return 0, false
}
