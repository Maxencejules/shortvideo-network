package fetch

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// errUnexpectedDial is returned when the HTTP transport asks for any
// address other than the session's endpoint (for example a proxy or a
// redirect target). It never happens with this package's transport.
var errUnexpectedDial = errors.New("dial target is not the session endpoint")

// errSocketMismatch is returned by the dialer's Control hook when the socket
// is about to connect to an address other than the vetted one.
var errSocketMismatch = errors.New("socket address is not the vetted address")

// dialError marks errors from the connect phase.
type dialError struct{ err error }

func (e *dialError) Error() string { return "connect: " + e.err.Error() }
func (e *dialError) Unwrap() error { return e.err }

// dialVetted connects to exactly target. The address is a literal, so the
// dialer does no lookup; the Control hook checks the final socket address
// before connect(2) and refuses anything else.
func (c *Client) dialVetted(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	network := "tcp6"
	if target.Addr().Is4() {
		network = "tcp4"
	}
	d := net.Dialer{Timeout: c.opts.Limits.Connect, Control: controlFor(target)}
	return d.DialContext(ctx, network, target.String())
}

// controlFor returns a net.Dialer Control hook that permits only target.
// The hook sees the final socket address just before connect(2).
func controlFor(target netip.AddrPort) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		got, err := netip.ParseAddrPort(address)
		if err != nil || got.Addr().Unmap() != target.Addr() || got.Port() != target.Port() {
			return errSocketMismatch
		}
		return nil
	}
}

// idleConn sets a read and write deadline before every Read and Write, so a
// peer that stops sending for longer than idle ends the request.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *idleConn) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
