package cli

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// pinnedModules are the dependency versions version prints: the chain
// framework pins of chain/go.mod.
var pinnedModules = []string{
	"github.com/cosmos/cosmos-sdk",
	"github.com/cosmos/cosmos-sdk/store/v2",
	"github.com/cometbft/cometbft",
}

func versionCmd(upgrades []upgrade.Upgrade, readBuildInfo func() (*debug.BuildInfo, bool)) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build revision, the Go and pinned module versions, and the compiled upgrade handlers",
		Long: `Print one "name value" line per field:

  svnd <main module version>
  go <Go version>
  vcs.revision <commit the binary was built from>
  vcs.modified <true when the checkout had uncommitted changes>
  <pinned module path> <version>   (cosmos-sdk, store/v2, cometbft)
  upgrades none, or one line per compiled upgrade handler:
  upgrade <name> from_app_version <n> to_app_version <n+1>

vcs.revision and vcs.modified come from the Go build information. They read
"unknown" when the binary was built outside a version-control checkout or
with -buildvcs=false.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, ok := readBuildInfo()
			if !ok {
				return fmt.Errorf("build info is unavailable")
			}
			return writeVersion(cmd.OutOrStdout(), info, upgrades)
		},
	}
}

// writeVersion writes the version lines of info and upgrades to w.
func writeVersion(w io.Writer, info *debug.BuildInfo, upgrades []upgrade.Upgrade) error {
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	known := func(value string) string {
		if value == "" {
			return "unknown"
		}
		return value
	}
	lines := []string{
		"svnd " + info.Main.Version,
		"go " + info.GoVersion,
		"vcs.revision " + known(settings["vcs.revision"]),
		"vcs.modified " + known(settings["vcs.modified"]),
	}
	for _, path := range pinnedModules {
		version := "unknown"
		for _, dep := range info.Deps {
			if dep.Path != path {
				continue
			}
			version = dep.Version
			if dep.Replace != nil {
				version += " => " + dep.Replace.Path + " " + dep.Replace.Version
			}
		}
		lines = append(lines, path+" "+version)
	}
	if len(upgrades) == 0 {
		lines = append(lines, "upgrades none")
	}
	for _, up := range upgrades {
		lines = append(lines, fmt.Sprintf("upgrade %s from_app_version %d to_app_version %d", up.Name, up.FromAppVersion, up.ToAppVersion))
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}
