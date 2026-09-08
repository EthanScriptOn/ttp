package release

import (
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint returns the stable identity used to reject duplicate release
// plans in durable storage. Execution status and logs are intentionally not
// part of the fingerprint, so retrying a completed release remains possible.
func Fingerprint(item Release) string {
	digest := sha256.Sum256([]byte(releaseDuplicateKey(item)))
	return hex.EncodeToString(digest[:])
}
