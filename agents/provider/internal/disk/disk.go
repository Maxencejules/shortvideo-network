package disk

import (
	"fmt"
	"syscall"
)

// Available is the free byte count statfs reports for path. A negative
// block size or a product that does not fit in uint64 is an error, and the
// caller refuses to stage.
func Available(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	if st.Bsize <= 0 {
		return 0, fmt.Errorf("disk: block size %d", st.Bsize)
	}
	bsize := uint64(st.Bsize)
	if st.Bavail > 0 && bsize > ^uint64(0)/st.Bavail {
		return 0, fmt.Errorf("disk: available bytes overflow")
	}
	return st.Bavail * bsize, nil
}
