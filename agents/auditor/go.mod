module github.com/Maxencejules/shortvideo-network/agents/auditor

go 1.26.5

toolchain go1.26.10

require github.com/Maxencejules/shortvideo-network/chain v0.0.0

replace (
	github.com/Maxencejules/shortvideo-network/chain => ../../chain
	github.com/Maxencejules/shortvideo-network/protocol => ../../proto
)
