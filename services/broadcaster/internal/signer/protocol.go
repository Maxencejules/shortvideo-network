package signer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// The signer speaks one request and one response per connection over its
// Unix socket. Each is a frame: a 4-byte big-endian length, then that many
// bytes of JSON. Unknown JSON fields and trailing data are refused.
const (
	// MaxRequestBytes bounds a request frame: a message of at most
	// genesis.MaxTxBytes in base64, plus the envelope fields.
	MaxRequestBytes = 256 << 10
	// MaxResponseBytes bounds a response frame: a transaction of at most
	// genesis.MaxTxBytes in base64, or the key list.
	MaxResponseBytes = 256 << 10
	// ProtocolVersion is the only request version.
	ProtocolVersion = 1
)

// Operations.
const (
	OpSign = "sign"
	OpKeys = "keys"
)

// Signer-level refusal codes (the policy codes cover every check of a
// message).
const (
	CodePeerRefused policy.Code = "PEER_REFUSED"
	CodeUnknownKey  policy.Code = "UNKNOWN_KEY"
	CodeMalformed   policy.Code = "MALFORMED_REQUEST"
	CodeOverloaded  policy.Code = "OVERLOADED"
	CodeInternal    policy.Code = "INTERNAL"
)

// request is the wire form of a request.
type request struct {
	Version int    `json:"v"`
	Op      string `json:"op"`
	// The fields below are for OpSign only.
	KeyName       string `json:"key_name,omitempty"`
	ChainID       string `json:"chain_id,omitempty"`
	AccountNumber uint64 `json:"account_number,omitempty"`
	Sequence      uint64 `json:"sequence,omitempty"`
	Gas           uint64 `json:"gas,omitempty"`
	TimeoutHeight uint64 `json:"timeout_height,omitempty"`
	// TypeURL and Value are the one message as a protobuf Any: Value is
	// the deterministic protobuf encoding of the message.
	TypeURL string `json:"type_url,omitempty"`
	Value   []byte `json:"value,omitempty"`
}

// refusalBody is the wire form of a refusal.
type refusalBody struct {
	Code   policy.Code `json:"code"`
	Detail string      `json:"detail,omitempty"`
}

// keyBody is the wire form of one key's public identity.
type keyBody struct {
	Name      string       `json:"name"`
	Class     policy.Class `json:"class"`
	Algorithm string       `json:"algorithm"`
	Address   []byte       `json:"address"`
	PublicKey []byte       `json:"public_key"`
}

// response is the wire form of a response: exactly one of Refusal, Keys,
// or TxBytes with TxHash is set.
type response struct {
	Refusal *refusalBody `json:"refusal,omitempty"`
	Keys    []keyBody    `json:"keys,omitempty"`
	TxBytes []byte       `json:"tx_bytes,omitempty"`
	TxHash  []byte       `json:"tx_hash,omitempty"`
}

// writeFrame writes v as one frame.
func writeFrame(w io.Writer, v any, limit int) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("signer: encode frame: %w", err)
	}
	if len(body) > limit {
		return fmt.Errorf("signer: frame of %d bytes above %d", len(body), limit)
	}
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame, uint32(len(body)))
	copy(frame[4:], body)
	_, err = w.Write(frame)
	return err
}

// readFrame reads one frame of at most limit bytes and decodes it into v,
// refusing unknown fields and trailing data.
func readFrame(r io.Reader, v any, limit int) error {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return fmt.Errorf("signer: read frame length: %w", err)
	}
	n := binary.BigEndian.Uint32(head[:])
	if n == 0 || n > uint32(limit) {
		return fmt.Errorf("signer: frame length %d outside 1 to %d", n, limit)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return fmt.Errorf("signer: read frame: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("signer: decode frame: %w", err)
	}
	if dec.More() {
		return errors.New("signer: data after the frame's JSON document")
	}
	return nil
}
