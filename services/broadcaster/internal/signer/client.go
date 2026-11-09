package signer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// Client bounds.
const (
	MinClientTimeout = time.Second
	MaxClientTimeout = 2 * MaxRPCTimeout
)

// KeyInfo is the public identity of one signer key: what serve needs to
// name a signer, pin it to an intent, publish it (GET /v1/signers), and
// build a simulation transaction. It holds no secret.
type KeyInfo struct {
	Name      string
	Class     policy.Class
	Algorithm string
	Address   []byte
	PubKey    cryptotypes.PubKey
}

// SignRequest asks for one outer transaction.
type SignRequest struct {
	KeyName       string
	AccountNumber uint64
	Sequence      uint64
	Gas           uint64
	TimeoutHeight uint64
	// Msg is the one message, with its signer and business id set.
	Msg sdk.Msg
}

// Signed is a transaction the signer returned and the client verified.
type Signed struct {
	Bytes []byte
	Hash  [32]byte
}

// Client is serve's connection to the signer: one Unix socket connection
// per request. It verifies every signed transaction it receives against
// the request (see RequestSignature) and runs the allowlist check before it asks.
type Client struct {
	socket  string
	timeout time.Duration
	codec   *chain.Codec
	enc     app.Encoding
	mu      sync.Mutex
	keys    map[string]KeyInfo
}

// NewClient returns a client of the signer at socketPath for codec's chain
// id. timeout bounds each request (MinClientTimeout to MaxClientTimeout).
func NewClient(socketPath string, codec *chain.Codec, timeout time.Duration) (*Client, error) {
	if socketPath == "" || len(socketPath) > maxSocketPath || codec == nil {
		return nil, errors.New("signer: client needs a socket path and a codec")
	}
	if timeout < MinClientTimeout || timeout > MaxClientTimeout {
		return nil, fmt.Errorf("signer: client timeout must be %s to %s", MinClientTimeout, MaxClientTimeout)
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		return nil, fmt.Errorf("signer: encoding: %w", err)
	}
	return &Client{socket: socketPath, timeout: timeout, codec: codec, enc: enc}, nil
}

// call sends req and returns the response. A refusal is a
// *policy.Refusal.
func (c *Client) call(ctx context.Context, req request) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return response{}, fmt.Errorf("signer: dial: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return response{}, fmt.Errorf("signer: deadline: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	// A signer that refuses the peer answers without reading the request
	// and closes, so the write can fail with a broken pipe while the
	// refusal waits to be read: read the answer even after a failed write.
	writeErr := writeFrame(conn, req, MaxRequestBytes)
	var resp response
	readErr := readFrame(conn, &resp, MaxResponseBytes)
	if readErr == nil && resp.Refusal != nil {
		return response{}, &policy.Refusal{Code: resp.Refusal.Code, Detail: resp.Refusal.Detail}
	}
	if writeErr != nil {
		return response{}, writeErr
	}
	if readErr != nil {
		return response{}, readErr
	}
	return resp, nil
}

// Keys asks the signer for its keys' public identities and remembers them.
func (c *Client) Keys(ctx context.Context) ([]KeyInfo, error) {
	resp, err := c.call(ctx, request{Version: ProtocolVersion, Op: OpKeys})
	if err != nil {
		return nil, err
	}
	if len(resp.Keys) == 0 || len(resp.Keys) > 2 {
		return nil, errors.New("signer: keys answer without one or two keys")
	}
	out := make([]KeyInfo, 0, len(resp.Keys))
	known := map[string]KeyInfo{}
	for _, kb := range resp.Keys {
		info, err := keyInfoOf(kb)
		if err != nil {
			return nil, err
		}
		if _, dup := known[info.Name]; dup {
			return nil, errors.New("signer: keys answer names one key twice")
		}
		known[info.Name] = info
		out = append(out, info)
	}
	c.mu.Lock()
	c.keys = known
	c.mu.Unlock()
	return out, nil
}

func keyInfoOf(kb keyBody) (KeyInfo, error) {
	var pub cryptotypes.PubKey
	switch kb.Algorithm {
	case keys.Secp256k1:
		if len(kb.PublicKey) != sdksecp.PubKeySize {
			return KeyInfo{}, errors.New("signer: secp256k1 public key of the wrong size")
		}
		pub = &sdksecp.PubKey{Key: bytes.Clone(kb.PublicKey)}
	case keys.Ed25519:
		if len(kb.PublicKey) != sdked.PubKeySize {
			return KeyInfo{}, errors.New("signer: ed25519 public key of the wrong size")
		}
		pub = &sdked.PubKey{Key: bytes.Clone(kb.PublicKey)}
	default:
		return KeyInfo{}, errors.New("signer: key of an unknown algorithm")
	}
	if _, ok := policy.RequiredRole(kb.Class); !ok || kb.Name == "" || !bytes.Equal(pub.Address(), kb.Address) {
		return KeyInfo{}, errors.New("signer: key answer with an unknown class, no name, or an address that is not its key's")
	}
	return KeyInfo{Name: kb.Name, Class: kb.Class, Algorithm: kb.Algorithm, Address: bytes.Clone(kb.Address), PubKey: pub}, nil
}

// key returns the remembered identity of name, asking the signer once if
// it is not known yet.
func (c *Client) key(ctx context.Context, name string) (KeyInfo, error) {
	c.mu.Lock()
	info, ok := c.keys[name]
	c.mu.Unlock()
	if ok {
		return info, nil
	}
	if _, err := c.Keys(ctx); err != nil {
		return KeyInfo{}, err
	}
	c.mu.Lock()
	info, ok = c.keys[name]
	c.mu.Unlock()
	if !ok {
		return KeyInfo{}, &policy.Refusal{Code: CodeUnknownKey, Detail: "the signer has no key with this name"}
	}
	return info, nil
}

// RequestSignature asks the signer process to sign req; the client itself
// holds no key and signs nothing. Before asking it runs the allowlist check
// of the key's class (policy.CheckSignable, plan 7.1: checked in policy and
// again in the signer), so a disallowed request never reaches the socket.
// It accepts the answer only when the transaction it would have built
// itself from req, with the returned signature, is byte for byte the
// returned transaction: one message equal to req.Msg, the key as signer,
// req's account number, sequence, gas and timeout height, the configured
// chain id, and a signature that verifies.
func (c *Client) RequestSignature(ctx context.Context, req SignRequest) (Signed, error) {
	info, err := c.key(ctx, req.KeyName)
	if err != nil {
		return Signed{}, err
	}
	if err := policy.CheckSignable(info.Class, info.Address, req.Msg); err != nil {
		return Signed{}, err
	}
	m, ok := req.Msg.(proto.Message)
	if !ok {
		return Signed{}, errors.New("signer: message is not a protocol message")
	}
	value, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		return Signed{}, fmt.Errorf("signer: encode message: %w", err)
	}
	in := chain.TxInput{AccountNumber: req.AccountNumber, Sequence: req.Sequence, Gas: req.Gas, TimeoutHeight: req.TimeoutHeight}
	unsigned, err := c.codec.Prepare(req.Msg, info.PubKey, in)
	if err != nil {
		return Signed{}, err
	}
	resp, err := c.call(ctx, request{
		Version: ProtocolVersion, Op: OpSign, KeyName: req.KeyName, ChainID: c.codec.ChainID(),
		AccountNumber: req.AccountNumber, Sequence: req.Sequence, Gas: req.Gas, TimeoutHeight: req.TimeoutHeight,
		TypeURL: sdk.MsgTypeURL(req.Msg), Value: value,
	})
	if err != nil {
		return Signed{}, err
	}
	if len(resp.TxBytes) == 0 || uint64(len(resp.TxBytes)) > genesis.MaxTxBytes || len(resp.TxHash) != 32 {
		return Signed{}, errors.New("signer: answer without a transaction and its 32-byte hash")
	}
	signature, err := c.signatureOf(resp.TxBytes)
	if err != nil {
		return Signed{}, err
	}
	raw, hash, err := unsigned.Assemble(signature)
	if err != nil {
		return Signed{}, fmt.Errorf("signer: returned signature: %w", err)
	}
	if !bytes.Equal(raw, resp.TxBytes) || !bytes.Equal(hash[:], resp.TxHash) {
		return Signed{}, errors.New("signer: returned transaction is not the requested one")
	}
	return Signed{Bytes: raw, Hash: hash}, nil
}

// signatureOf returns the only SIGN_MODE_DIRECT signature of raw.
func (c *Client) signatureOf(raw []byte) ([]byte, error) {
	tx, err := c.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return nil, fmt.Errorf("signer: returned transaction does not decode: %w", err)
	}
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return nil, errors.New("signer: returned transaction has no signatures")
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 {
		return nil, errors.New("signer: returned transaction does not carry exactly one signature")
	}
	data, ok := sigs[0].Data.(*signingtypes.SingleSignatureData)
	if !ok || data.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
		return nil, errors.New("signer: returned signature is not SIGN_MODE_DIRECT")
	}
	return bytes.Clone(data.Signature), nil
}
