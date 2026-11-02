package genesis

// Protocol v0 denial-of-service bounds. Each constant below that has a key in
// docs/protocol/bounds.json matches that key, and TestBoundsMatchProtocolFile
// reads the file and fails if one drifts. Three constants have no bounds.json
// key: ProtocolVersion and MinValidators follow the spec machine
// (protocol_version 1, MIN_VALIDATOR_COUNT 4), and MaxChainIDLen is the
// CometBFT chain id limit, which the same test compares. They are not
// sponsorship allowances. DEC-005 stays unset.
const (
	ProtocolVersion = 1

	AccountBytes      = 20
	ConsensusKeyBytes = 32
	DigestBytes       = 32
	MinValidators     = 4
	MaxChainIDLen     = 50
	UnixSecondsMin    = 1
	UnixSecondsMax    = 253402300799

	// SafeIntegerMax is bounds.json safe_integer_max, 2^53-1. The total
	// validator power must not exceed it.
	SafeIntegerMax int64 = 9007199254740991

	MaxTxBytes  uint64 = 65536
	MaxGasPerTx uint64 = 20000000
	MinGasPerTx uint64 = 1

	MaxPackageBytes          uint64 = 268435456
	MaxEvidenceBatch         uint32 = 32
	MaxExpiryStepsPerBlock   uint32 = 128
	MaxPageLimit             uint32 = 100
	MaxOpenOffersPerProvider uint32 = 1024

	DevnetChainIDPrefix = "shortvideo-dev-"
	DevnetTestLabel     = "PROPOSED_TEST_PARAMETER"
	IDPattern           = `^[a-z0-9][a-z0-9._-]{0,63}$`
)

// DecisionIDs are the blocked parameters. None is approved on chain.
var DecisionIDs = []string{
	"DEC-001",
	"DEC-002",
	"DEC-003",
	"DEC-004",
	"DEC-005",
	"DEC-006",
	"DEC-007",
	"DEC-008",
}
