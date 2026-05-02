package daemon

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ulidEncoding is Crockford's base32 (no I, L, O, U) — the ULID alphabet.
const ulidEncoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a sortable, time-prefixed identifier compatible with the
// design doc's filename convention. We avoid a third-party ULID dependency
// because the daemon scaffold is intentionally minimal.
//
// Format: 10 timestamp chars (48 bits, ms since epoch) + 16 random chars
// (80 bits). Total 26 chars, all uppercase Crockford base32.
func NewULID() string {
	return newULIDAt(time.Now())
}

func newULIDAt(t time.Time) string {
	ms := uint64(t.UnixMilli())
	var ts [10]byte
	for i := 9; i >= 0; i-- {
		ts[i] = ulidEncoding[ms&0x1f]
		ms >>= 5
	}

	var random [10]byte
	if _, err := rand.Read(random[:]); err != nil {
		// Failing to read crypto/rand is essentially impossible on Linux; we
		// panic because returning a zeroed ULID would silently break ordering.
		panic(fmt.Sprintf("daemon: read random for ULID: %v", err))
	}
	var rnd [16]byte
	// Pack 80 random bits into 16 base32 chars by reading 5 bits at a time.
	for i := 0; i < 16; i++ {
		bitOffset := i * 5
		byteIdx := bitOffset / 8
		bitIdx := bitOffset % 8
		// Pull 16 bits at byteIdx and shift to extract 5.
		var window uint16
		window = uint16(random[byteIdx]) << 8
		if byteIdx+1 < len(random) {
			window |= uint16(random[byteIdx+1])
		}
		shift := 11 - bitIdx
		rnd[i] = ulidEncoding[(window>>shift)&0x1f]
	}

	return string(ts[:]) + string(rnd[:])
}
