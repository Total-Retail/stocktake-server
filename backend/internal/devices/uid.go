package devices

import (
	"errors"
	"strings"
)

// ErrBadUID is returned for a card reading that is not a hex UID of a known length.
var ErrBadUID = errors.New("card UID must be hexadecimal, 4, 7, 8 or 10 bytes (8, 14, 16 or 20 hex digits); " +
	"a 10-digit number is a decimal reading — set the USB reader to output hex")

// NormalizeUID turns a card reading into the stored form: upper-case hex, no separators.
// Accepts what phones and USB readers produce: "04:a1:b2:c3:d4:e5:f6", "04 A1 B2 ...",
// "04a1b2c3d4e5f6", optionally prefixed "0x".
func NormalizeUID(raw string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "0X")
	s = strings.NewReplacer(":", "", " ", "", "-", "", ".", "").Replace(s)
	switch len(s) {
	case 8, 14, 16, 20: // 4-byte (MIFARE Classic), 7-byte (NTAG, DESFire), 8-byte (ISO 15693, FeliCa), 10-byte
	default:
		return "", ErrBadUID
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'F') {
			return "", ErrBadUID
		}
	}
	return s, nil
}
