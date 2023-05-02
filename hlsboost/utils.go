package hlsboost

import (
	"crypto"
	crand "crypto/rand"
	"encoding/hex"
	mrand "math/rand"
)

func md5Short(s string) string {
	m := crypto.MD5.New()
	m.Write([]byte(s))
	hash := m.Sum(nil)
	return hex.EncodeToString(hash[:8])
}

func genId() string {
	buf := make([]byte, 8)
	if _, err := crand.Read(buf); err != nil {
		mrand.Read(buf)
	}
	return hex.EncodeToString(buf)
}
