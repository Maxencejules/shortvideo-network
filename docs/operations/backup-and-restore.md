# Backups, restoration and signer safety

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. No person is assigned. Retention, key custody, and recovery policy are placeholders. The retention period is unset. No service recovery-time or recovery-point target is set. A number printed by the drill is a measurement of that drill.

The blueprint section is [Backups and recovery](../blueprint.md#backups-and-recovery). The chain restore that goal 10 already allows, for one diverged node from its own copy taken at a halt, stays in [failed-upgrade.md](failed-upgrade.md#restore-a-lone-diverged-node). This procedure does not replace that one. It does not start a chain.

## What is backed up

`svn-restore backup` writes a directory of mode 0700. Each file is mode 0600. The parts, in the only restore order, are:

1. Non-secret configuration.
2. A logical database dump.
3. Signed records.
4. Critical media.
5. An index checkpoint, or a canonical-state export of the same shape.
6. Signer-safety metadata: the consensus address and the last height, round, and step.

The database dump, the signed records, and the media are encrypted with AES-256-GCM. The key file is written outside the backup directory. The backup does not contain a validator signing key, a peer identity, a governance key, a service signing key, or the encryption key. `svn-restore export-ci` writes the manifest only. A file whose name is a live secret, or a PEM header in a plaintext part, refuses that export.

A directory whose index source is a live replica is refused. A live replica is not a historical backup. Provider copies of the current package are not the database, the signer state, or the encryption key.

## Restore

`svn-restore restore` requires the origin path to be absent. It checks the format version, the restore order, the file digests, and the encryption tag before it writes the destination. A wrong version is refused. A bit error that survives a rewritten file digest still fails the encryption tag. A missing part is refused unless `--partial` is set, and a partial result is not reported as restored.

The index is rebuilt by copying the checkpoint rows. Rows that the checkpoint does not name are not invented from media files that happen to be present. This restore does not dial a node and does not replay a block store. Replaying a block store needs the chain, which this harness treats as unavailable.

Playback after restore returns the media bytes only when the restored authorization is published, the digest matches, and admissions are not stopped. A removed row is refused. The media bytes of a removed row are kept. History is not marked erased. The retention period is still unset.

## Signer safety

The selected implementation is CometBFT. Its signing-state file holds a height, a round, and a step. Height is a decimal string. This tool reads that shape.

A live signer holds an exclusive lock outside the node home, named by the consensus address. The lock is not inside the home, so a copied home does not carry it. A second process that presents the same consensus public key is refused while the lock is held. Two processes on this machine are not two machines. A second machine is not covered here.

Replacement reads the stop attestation written when the old process exits cleanly. The attestation has to name the same consensus address, and the recorded pid has to be gone. A process that was killed does not write the attestation, and replacement is refused. The next height, round, and step must not be behind the attestation or behind the state already in the replacement home. A snapshot whose height is behind the recorded chain head is not permission to sign. The tool does not write a signing permission in that case. An inspection file can hold the metadata without enabling signing.

Peer identity, validator signing, and governance material must be three paths and three different byte strings. A copied validator file is refused. A copy presented while the address lock is held is refused.

This is one machine. It is not a proof that a second computer cannot start the same key. No independent reviewer has signed this procedure.

## Rotation, recovery, and deletion

`svn-restore rotate-service` generates a new service signing key in memory and writes the public key only. Without `--drill` or an operator token it refuses. The token does not name a person. The retired public key does not verify a new signature. Production rotation is not performed by the drill.

Account export bytes can be encrypted and decrypted with a key generated for the drill. Completing account recovery, which would bind a new authenticator, is refused. The custody decision does not name that procedure.

`svn-restore delete` removes one backup directory only after `--confirm delete` and either `--drill` or an operator token. It refuses a symlink inside the directory. The result records that history was not erased and that the retention period is unset. The command does not apply an age rule, because no period is approved.

Stopped admissions are a marker on the restored directory. Playback honours it. A lease handoff file names the two providers and the lease id and records that no chain transaction was submitted.

## What a replica cannot rebuild

A current media replica does not hold the validator signing state, the backup encryption key, account sessions, uncommitted quarantine uploads, moderation complaint text, pruned chain blocks, or package versions that are absent from that replica. Restoring those from the replica is not attempted.

## Compatibility

The backup format is `SVN-BACKUP-1`, format version 1, application schema version 1. Any other version is refused. No migration runs. A newer or older backup is not adapted in place.

## Commands

```text
svn-restore backup --world DIR --out DIR --key-out FILE
svn-restore restore --backup DIR --key FILE --dest DIR --origin PATH
svn-restore export-ci --backup DIR --out DIR
svn-restore delete --backup DIR --confirm delete --drill
svn-restore rehearse --work DIR
svn-restore rotate-service --drill --public-out FILE
svn-restore signer-hold --home DIR --lock-dir DIR
svn-restore signer-replace --old DIR --new DIR --lock-dir DIR --height N --round N --step N --chain-head N
```

`rehearse` prints the observed recovery time and the observed age of the newest record. Both lines say they are not service guarantees. The drill uses a disposable database cluster, synthetic media bytes, and keys generated in the process. Those keys are not written into the repository.

## Approvals still required

- The retention period is unset.
- The key custody procedure is unset.
- The recovery policy, and any service recovery-time or recovery-point target, is unset.
- No operator is named who can approve a production restore or a production key rotation.
- Account recovery completion waits on the custody decision.
- Signer replacement on a live node needs an independent review.

Until those are decided, a drill is not a live recovery.
