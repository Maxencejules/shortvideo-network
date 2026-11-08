#!/usr/bin/env bash
# Run a command in a private network namespace with only loopback up, so
# fixed devnet ports (26656+ / 26657+ / 27101+) cannot collide with another
# devnet run on the host. No outbound network inside (Go modules must be cached).
set -euo pipefail
exec unshare --net -- /usr/bin/python3 -c '
import fcntl, socket, struct, os, sys
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
ifr = struct.pack("16sh", b"lo", 0)
flags = struct.unpack("16sh", fcntl.ioctl(s, 0x8913, ifr))[1]  # SIOCGIFFLAGS
fcntl.ioctl(s, 0x8914, struct.pack("16sh", b"lo", flags | 1))   # SIOCSIFFLAGS IFF_UP
s.close()
os.execvp(sys.argv[1], sys.argv[1:])
' "$@"
