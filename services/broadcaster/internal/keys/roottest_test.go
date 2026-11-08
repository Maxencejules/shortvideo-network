//go:build svn_roottest

package keys

// Root-only ownership tests (plan 4.1, 13.6; DR-11-35), run only with
// -tags svn_roottest by the root uid-boundary capture, never in the
// default build. A key file or directory owned by another uid needs a
// chown to that uid, which only root can do; the default build checks the
// owner comparison through the expected-owner case of
// TestKeyFileRules. They skip with a reason when the test
// process is not root. The other uid (4242) owns the files only inside the
// test's temporary directory, which is removed at the end.

import (
	"errors"
	"os"
	"testing"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
)

// TestLoadRefusesOtherOwner: a key file, or a key directory, owned by a uid
// other than the expected one is refused even with the right modes.
func TestLoadRefusesOtherOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("skipped: needs euid 0 to chown a key file or directory to another uid")
	}
	good := sdksecp.GenPrivKey()
	t.Run("file owned by another uid", func(t *testing.T) {
		path := writeKeyFile(t, keyDir(t), "k.hex", good, FileMode)
		if err := os.Lchown(path, 4242, 4242); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
	t.Run("directory owned by another uid", func(t *testing.T) {
		dir := keyDir(t)
		path := writeKeyFile(t, dir, "k.hex", good, FileMode)
		if err := os.Lchown(dir, 4242, 4242); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path, self); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Load = %v, want ErrUnsafe", err)
		}
	})
}
