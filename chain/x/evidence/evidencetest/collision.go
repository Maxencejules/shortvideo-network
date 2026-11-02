package evidencetest

import "encoding/hex"

// prefixCollision is two distinct 32-byte strings whose SHA-256 digests
// share their first 8 bytes and differ in byte 8, so
// evidence.AuditorAddress gives two different 20-byte addresses with one
// evidence.IDPrefix ("5a48c07a2b7ecf90."). Each is the 24 ASCII bytes
// "svn-evidence-prefix-coll" followed by 8 search bytes. They were found
// once, off chain, by a parallel distinguished-point collision search over
// x -> SHA-256("svn-evidence-prefix-coll" || big-endian x)[:8] (about
// 3.2e9 evaluations); TestPrefixCollisionKeys checks the property.
//
// SYNTHETIC TEST DATA. They are 32-byte values with no known private key,
// not ed25519 keys of any party: no signature can be made under them, so
// they serve only where a record or an address is checked without a
// signature (genesis documents, stored records, address rules).
var prefixCollision = [2]string{
	"73766e2d65766964656e63652d7072656669782d636f6c6cab97f069ac84f771",
	"73766e2d65766964656e63652d7072656669782d636f6c6cd565c54e7b414cba",
}

// PrefixCollisionKeys returns fresh copies of the two synthetic 32-byte
// signer keys whose AuditorAddress values differ but share their first
// evidence.IDPrefixBytes bytes. Tests use them to reach a rule that the id
// prefix rule would otherwise shadow: the batch signer-key rule of
// ValidateGenesis and of CheckInvariants (I2), and the app genesis rule
// that refuses two AUDITOR addresses sharing their first 8 bytes.
func PrefixCollisionKeys() (a, b []byte) {
	return mustHex(prefixCollision[0]), mustHex(prefixCollision[1])
}

func mustHex(s string) []byte {
	raw, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return raw
}
