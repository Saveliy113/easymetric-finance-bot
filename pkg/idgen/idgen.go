package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

func Short() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
