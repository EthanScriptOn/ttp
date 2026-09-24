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

// FingerprintWithID keeps an explicit replacement release distinct from the
// historical snapshot it supersedes. The logical duplicate key remains the
// same for the release service, while durable storage gets a unique value for
// the database's release fingerprint constraint.
func FingerprintWithID(item Release) string {
	if item.ID == "" {
		return Fingerprint(item)
	}
	digest := sha256.Sum256([]byte(releaseDuplicateKey(item) + "\x00" + item.ID))
	return hex.EncodeToString(digest[:])
}
