package statecheck

import (
	"fmt"
	"io"
	"strings"
)

// WriteText writes the report as lines of text. Equal reports give equal
// bytes: the lines follow the report's order (stores and checks by name,
// modules sorted), and the home path is not printed, so the reports of
// several nodes of one chain at one height can be compared byte for byte.
//
//	chain_id <id>
//	height <h>
//	app_hash <hex>
//	binary app_version <P>
//	binary module <name> <version>          one line per module
//	binary upgrade <name> <from> <to>       one line per handler, or "binary upgrades none"
//	gate row <n> <outcome>
//	store <name> sha256 <hex> [account sequences]
//	i13 sha256 <hex>
//	check <name> ok | check <name> FAIL <error>
//	result PASS | result FAIL
func (r *Report) WriteText(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "chain_id %s\n", r.ChainID)
	fmt.Fprintf(&b, "height %d\n", r.Height)
	fmt.Fprintf(&b, "app_hash %x\n", r.AppHash)
	fmt.Fprintf(&b, "binary app_version %d\n", r.Binary.AppVersion)
	for _, name := range sortedNames(r.Binary.Modules) {
		fmt.Fprintf(&b, "binary module %s %d\n", name, r.Binary.Modules[name])
	}
	if len(r.Binary.Upgrades) == 0 {
		b.WriteString("binary upgrades none\n")
	}
	for _, up := range r.Binary.Upgrades {
		fmt.Fprintf(&b, "binary upgrade %s %d %d\n", up.Name, up.FromAppVersion, up.ToAppVersion)
	}
	fmt.Fprintf(&b, "gate row %d %s\n", r.Gate.Row, gateOutcome(r.Gate))
	for _, store := range r.Stores {
		fmt.Fprintf(&b, "store %s sha256 %x", store.Name, store.SHA256)
		if store.Sequences {
			b.WriteString(" account sequences")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "i13 sha256 %x\n", r.I13)
	for _, check := range r.Checks {
		if check.Err == nil {
			fmt.Fprintf(&b, "check %s ok\n", check.Name)
			continue
		}
		fmt.Fprintf(&b, "check %s FAIL %s\n", check.Name, oneLine(check.Err.Error()))
	}
	if r.Passed() {
		b.WriteString("result PASS\n")
	} else {
		b.WriteString("result FAIL\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func gateOutcome(g GateResult) string {
	switch {
	case g.Row == 1:
		return "start"
	case g.Row == 3:
		return fmt.Sprintf("start in the upgrade window: the next block applies %s (app version %d to %d)",
			g.Upgrade.Name, g.Upgrade.FromAppVersion, g.Upgrade.ToAppVersion)
	default:
		return "refused: " + oneLine(g.Refusal)
	}
}

// oneLine keeps a message on its report line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
