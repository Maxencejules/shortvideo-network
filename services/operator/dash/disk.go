package dash

import (
	"fmt"
	"syscall"
)

// availableBytes is the free space statfs reports for path. It is an
// observation of this machine. It is not a minimum for every operator.
func availableBytes(path string) (uint64, error) {
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
