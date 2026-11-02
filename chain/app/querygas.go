package app

import (
	"fmt"
	"strconv"
	"strings"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// QueryGasLimitOption is the node option that sets the query gas limit. It
// is the key the SDK uses in app.toml and as a start flag
// (server.FlagQueryGasLimit). svnd does not read app.toml; it reads
// query_gas_limit from node.json and passes it under this key in
// Config.AppOptions.
const QueryGasLimitOption = "query-gas-limit"

// DefaultQueryGasLimit is the gas one gRPC-routed query (ABCI query on a
// registered service path, or the node gRPC server) may consume when the
// node sets no limit. It is a PROPOSED node default (decision request
// DR-07-10), not consensus state: nodes may set different values without
// affecting the app hash. It equals the transaction gas cap
// genesis.MaxGasPerTx, so one query does at most the metered store work
// one transaction may do. The SDK's own default is unlimited (2^64-1).
//
// Every store read of a gRPC-routed query is metered against it (the
// baseapp query context). Raw store queries (ABCI query on /store/...)
// bypass the query router and are not metered by the SDK; this limit does
// not bound them. A query that runs out fails: over ABCI query as the SDK's
// recovered-panic result (codespace "undefined", code 111222); the node
// gRPC server's SDK interceptor turns the same panic into ErrOutOfGas
// (codespace sdk, code 11). It bounds work, not response size; the registries bound their responses by
// construction (x/providers MaxRetiredKeysPerProvider and the page caps).
const DefaultQueryGasLimit = genesis.MaxGasPerTx

// QueryGasLimit returns the query gas limit the node options select. No
// options, no QueryGasLimitOption, an empty string, or 0 select
// DefaultQueryGasLimit: unlike the SDK, 0 never means unlimited. Any other
// value must be a non-negative integer (a Go integer type or a decimal
// string); anything else is an error and the node does not start.
func QueryGasLimit(opts servertypes.AppOptions) (uint64, error) {
	if opts == nil {
		return DefaultQueryGasLimit, nil
	}
	var limit uint64
	switch v := opts.Get(QueryGasLimitOption).(type) {
	case nil:
	case uint64:
		limit = v
	case uint32:
		limit = uint64(v)
	case uint:
		limit = uint64(v)
	case int:
		if v < 0 {
			return 0, fmt.Errorf("%s %d is negative", QueryGasLimitOption, v)
		}
		limit = uint64(v)
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("%s %d is negative", QueryGasLimitOption, v)
		}
		limit = uint64(v)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			break
		}
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s %q: %w", QueryGasLimitOption, v, err)
		}
		limit = parsed
	default:
		return 0, fmt.Errorf("%s has type %T, want an integer", QueryGasLimitOption, v)
	}
	if limit == 0 {
		return DefaultQueryGasLimit, nil
	}
	return limit, nil
}
