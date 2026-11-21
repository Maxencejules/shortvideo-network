# svn-restore

Development backup and restore for the devnet beta. This is not a production procedure. Public launch is disabled.

The command backs up an application database dump, signed records, non-secret configuration, critical media, an index checkpoint, and signer-safety metadata. It encrypts the database, the signed records, and the media. It does not store a validator signing key, a peer key, a service signing key, or the backup encryption key inside the backup.

Retention, key custody, and recovery policy are placeholders. The period, the custodian, and any service recovery target are unset. A live replica is not accepted as a backup. Production restore and production key rotation refuse to run without an operator token, and this module does not name an operator.

`svn-restore rehearse --work DIR` runs the disposable drill on this machine. The observed recovery time and the observed data age it prints are measurements of that drill. They are not service guarantees.
