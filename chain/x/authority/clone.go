package authority

import (
	"bytes"

	"google.golang.org/protobuf/proto"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func cloneValidators(rows []*authorityv1.ValidatorPower) []*authorityv1.ValidatorPower {
	out := make([]*authorityv1.ValidatorPower, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*authorityv1.ValidatorPower)
	}
	return out
}

func cloneSubmitters(rows []*typesv1.Submitter) []*typesv1.Submitter {
	out := make([]*typesv1.Submitter, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*typesv1.Submitter)
	}
	return out
}

func cloneSeats(rows []*authorityv1.DevnetTestSeat) []*authorityv1.DevnetTestSeat {
	out := make([]*authorityv1.DevnetTestSeat, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*authorityv1.DevnetTestSeat)
	}
	return out
}

func cloneProposals(rows []*authorityv1.Proposal) []*authorityv1.Proposal {
	out := make([]*authorityv1.Proposal, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*authorityv1.Proposal)
	}
	return out
}

func clonePending(rows []*authorityv1.PendingValidatorPower) []*authorityv1.PendingValidatorPower {
	out := make([]*authorityv1.PendingValidatorPower, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*authorityv1.PendingValidatorPower)
	}
	return out
}

func cloneParams(rows []*typesv1.DevnetTestParameter) []*typesv1.DevnetTestParameter {
	out := make([]*typesv1.DevnetTestParameter, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*typesv1.DevnetTestParameter)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBytesMap(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for key, value := range in {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

func cloneUintMap(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneBytesList(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i, row := range in {
		out[i] = append([]byte(nil), row...)
	}
	return out
}

func cloneBytesListMap(in map[string][][]byte) map[string][][]byte {
	out := make(map[string][][]byte, len(in))
	for key, rows := range in {
		out[key] = cloneBytesList(rows)
	}
	return out
}

func cloneProviders(in map[string]*ProviderRecord) map[string]*ProviderRecord {
	out := make(map[string]*ProviderRecord, len(in))
	for key, row := range in {
		copyRow := *row
		copyRow.Account = append([]byte(nil), row.Account...)
		copyRow.SigningPub = append([]byte(nil), row.SigningPub...)
		copyRow.Endpoint = append([]byte(nil), row.Endpoint...)
		out[key] = &copyRow
	}
	return out
}

func sameBytes(a, b []byte) bool { return bytes.Equal(a, b) }
