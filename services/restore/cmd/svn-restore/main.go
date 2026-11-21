package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/services/restore/internal/restore"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "backup":
		return cmdBackup(args[1:])
	case "restore":
		return cmdRestore(args[1:])
	case "export-ci":
		return cmdExport(args[1:])
	case "delete":
		return cmdDelete(args[1:])
	case "rehearse":
		return cmdRehearse(args[1:])
	case "rotate-service":
		return cmdRotate(args[1:])
	case "signer-hold":
		return cmdHold(args[1:])
	case "signer-replace":
		return cmdReplace(args[1:])
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: svn-restore backup|restore|export-ci|delete|rehearse|rotate-service|signer-hold|signer-replace")
}

func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	world := fs.String("world", "", "world directory")
	out := fs.String("out", "", "backup directory")
	keyOut := fs.String("key-out", "", "encryption key file, outside the backup")
	if err := fs.Parse(args); err != nil || *world == "" || *out == "" || *keyOut == "" {
		return 2
	}
	if err := restore.RefuseLiveSecrets(*world); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	loaded, err := restore.LoadWorld(*world)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if _, err := restore.Backup(*out, loaded, restore.Options{KeyOut: *keyOut}); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Printf("BACKUP wrote=true secrets_included=false retention_period=%s\n", restore.RetentionPeriod)
	return 0
}

func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	backup := fs.String("backup", "", "backup directory")
	key := fs.String("key", "", "encryption key file")
	dest := fs.String("dest", "", "clean destination")
	origin := fs.String("origin", "", "origin path that must be absent")
	partial := fs.Bool("partial", false, "allow a partial restore")
	if err := fs.Parse(args); err != nil || *backup == "" || *key == "" || *dest == "" || *origin == "" {
		return 2
	}
	result, err := restore.Restore(*backup, *key, *dest, *origin, *partial)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Printf("RESTORE status=%s origin_unavailable=%t digests_match=%t observed_rto_seconds=%g observed_rpo_seconds=%g rto_rpo_are_service_guarantees=%t\n",
		result.Status, result.OriginUnavailable, result.DigestsMatch, result.ObservedRTOSeconds, result.ObservedRPOSeconds, result.RTOAndRPOAreServiceGuarantees)
	return 0
}

func cmdExport(args []string) int {
	fs := flag.NewFlagSet("export-ci", flag.ContinueOnError)
	backup := fs.String("backup", "", "backup directory")
	out := fs.String("out", "", "artifact directory")
	if err := fs.Parse(args); err != nil || *backup == "" || *out == "" {
		return 2
	}
	if err := restore.ExportCI(*backup, *out); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Println("EXPORT secrets_included=false")
	return 0
}

func cmdDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	backup := fs.String("backup", "", "backup directory")
	confirm := fs.String("confirm", "", "must be delete")
	drill := fs.Bool("drill", false, "drill approval")
	token := fs.String("approval", "", "operator token")
	if err := fs.Parse(args); err != nil || *backup == "" {
		return 2
	}
	result, err := restore.ControlledDelete(*backup, *confirm, restore.Approval{Drill: *drill, OperatorToken: *token})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Printf("DELETE history_erased=%t retention_period=%s\n", result.HistoryErased, result.RetentionPeriod)
	return 0
}

func cmdRehearse(args []string) int {
	fs := flag.NewFlagSet("rehearse", flag.ContinueOnError)
	work := fs.String("work", "", "work directory")
	if err := fs.Parse(args); err != nil || *work == "" {
		return 2
	}
	result, err := restore.Drill(*work)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Printf("DRILL status=%s digests_match=%t authorization_match=%t playback_after=%t removal_after=%t postgres=%t rows_match=%t\n",
		result.Status, result.DigestsMatch, result.AuthorizationMatch, result.PlaybackAfter, result.RemovalAfter, result.Postgres, result.RowsMatch)
	fmt.Printf("DRILL observed_rto_seconds=%g observed_rpo_seconds=%g rto_rpo_are_service_guarantees=%t\n",
		result.ObservedRTOSeconds, result.ObservedRPOSeconds, result.Guarantees)
	return 0
}

func cmdRotate(args []string) int {
	fs := flag.NewFlagSet("rotate-service", flag.ContinueOnError)
	drill := fs.Bool("drill", false, "drill approval")
	token := fs.String("approval", "", "operator token")
	out := fs.String("public-out", "", "public key file")
	if err := fs.Parse(args); err != nil || *out == "" {
		return 2
	}
	next, _, _, err := restore.RotateService(restore.Approval{Drill: *drill, OperatorToken: *token})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if err := os.WriteFile(*out, next, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	if err := os.Chmod(*out, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Printf("ROTATE old_rejected=true new_accepted=true operator_named=false bytes=%d\n", len(next))
	return 0
}

func cmdHold(args []string) int {
	fs := flag.NewFlagSet("signer-hold", flag.ContinueOnError)
	home := fs.String("home", "", "signer home")
	lockDir := fs.String("lock-dir", "", "address lock directory")
	if err := fs.Parse(args); err != nil || *home == "" || *lockDir == "" {
		return 2
	}
	if err := restore.Hold(*home, *lockDir, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		if err == restore.ErrDuplicateSigner {
			return 2
		}
		return 1
	}
	fmt.Println("SIGNER stopped=true")
	return 0
}

func cmdReplace(args []string) int {
	fs := flag.NewFlagSet("signer-replace", flag.ContinueOnError)
	old := fs.String("old", "", "old signer home")
	next := fs.String("new", "", "replacement home")
	lockDir := fs.String("lock-dir", "", "address lock directory")
	height := fs.String("height", "", "next height")
	round := fs.String("round", "0", "next round")
	step := fs.String("step", "0", "next step")
	head := fs.String("chain-head", "", "chain head height")
	sign := fs.Bool("sign", false, "allow signing when the state is not stale")
	if err := fs.Parse(args); err != nil || *old == "" || *next == "" || *lockDir == "" || *height == "" || *head == "" {
		return 2
	}
	state, err := parseState(*height, *round, *step)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	chainHead, err := strconv.ParseInt(*head, 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 2
	}
	if err := restore.Replace(*old, *next, *lockDir, state, chainHead, *sign); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	fmt.Println("SIGNER replace=accepted")
	return 0
}

func parseState(height, round, step string) (restore.SignState, error) {
	h, err := strconv.ParseInt(height, 10, 64)
	if err != nil {
		return restore.SignState{}, err
	}
	r, err := strconv.ParseInt(round, 10, 32)
	if err != nil {
		return restore.SignState{}, err
	}
	s, err := strconv.ParseInt(step, 10, 32)
	if err != nil {
		return restore.SignState{}, err
	}
	return restore.SignState{Height: h, Round: int32(r), Step: int32(s)}, nil
}
