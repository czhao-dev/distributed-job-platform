package reconciler

import (
	"crypto/rand"
	"encoding/hex"
)

func newPodID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pod_" + hex.EncodeToString(b), nil
}
