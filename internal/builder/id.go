package builder

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// Crockford's Base32 character set (excludes I, L, O, U to avoid confusion)
const base32Encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	idMu       sync.Mutex
	lastTimeMs int64
)

// GenerateBuildID generates a sortable, unique 26-character identifier (ULID style).
// Example: 01K5XXXXXXXXXXXXXXX...
func GenerateBuildID() string {
	idMu.Lock()
	defer idMu.Unlock()

	nowMs := time.Now().UnixMilli()
	if nowMs <= lastTimeMs {
		nowMs = lastTimeMs + 1
	}
	lastTimeMs = nowMs

	// 10 chars for 48-bit timestamp (Base32)
	timeChars := make([]byte, 10)
	t := nowMs
	for i := 9; i >= 0; i-- {
		timeChars[i] = base32Encoding[t%32]
		t /= 32
	}

	// 16 chars for 80-bit randomness
	randBytes := make([]byte, 10)
	if _, err := rand.Read(randBytes); err != nil {
		// Fallback deterministic if crypto/rand fails
		for i := range randBytes {
			randBytes[i] = byte(time.Now().UnixNano() >> (i * 8))
		}
	}

	randChars := make([]byte, 16)
	for i := 0; i < 16; i++ {
		b := randBytes[i%len(randBytes)]
		randChars[i] = base32Encoding[int(b)%32]
	}

	return fmt.Sprintf("%s%s", string(timeChars), string(randChars))
}
