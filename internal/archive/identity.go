package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func anchor(id, sender string, from bool) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%t", id, sender, from)))
	return "msg-" + hex.EncodeToString(h[:])
}

func digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
