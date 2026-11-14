# Publication recovery and durable subtitle follow-ups

Baseline: `Surc/miyabi` commit `01d6945` (2026-11-14).

## Implemented flow

- `scrape.prepareMetadata` checkpoints candidates in task payloads only. Visible
  Movie metadata and actor/tag indexes change in the final publish transaction.
- The optional `BeginAttempt` hook pins engine/configuration/options to a
  nonsecret attempt stamp. Old unstamped candidates are rejected when enabled.
  Completed replay skips online health checks. Image configuration conflicts
  escape selection immediately; native optional metadata fallback may still
  tolerate an ambiguous JavDB result when usable primary artwork exists.
- Production scrape publication stages only generated sidecars and explicitly
  database-owned subtitles, never original videos or an entire media directory.
  A private durable catalog records before/after hashes and recovery blobs.
  STRM installation follows other artifacts. Known subtitle path migrations
  and the completed task's operation ID/digest receipt commit with Movie state.
- SQLite receipt readback, not the transaction call's return value alone,
  decides commit versus rollback. The completion callback rereads durable task
  state: a post-commit cleanup failure cannot downgrade the published Movie,
  even when its caller holds an older task payload. Missing/corrupt/mismatched receipts block
  further export writes without deleting the catalog. The shared Manager lock
  holds throughout publication/recovery. Startup recovers before starting
  export writers; later writes retry recovery under the same lock.
- Scan, STRM rewriting, subtitle export and manual subtitle changes use the
  shared write barrier. They cannot bypass an unresolved scrape publication;
  they are not each implemented as a separate scrape publication transaction.
- A read-only atomic status snapshot exposes only `blocked` and `pending`.
  Status requests never trigger file recovery or expose paths/errors/secrets.
- Version-specific subtitle tasks are inserted in the same SQLite transaction
  as successful publication. Their own pool supports restart, pause, bounded
  retry, and visible manual retry. Subtitle failures do not undo Movie import.
  Pending full-video targets deduplicate; fragments are not assigned a full
  movie subtitle. Notification failures retry even when a track already exists.

## Operational boundaries

- Preserve both the application database and private publication catalog/stage
  when diagnosing a blocked publication. Do not delete task receipts while a
  publication remains pending. Current production code has no task purge path.
- Recovery protects detected external changes and refuses unknown state.
  The filesystem is not a transactional CAS service: external processes must
  not concurrently modify managed paths during publication. No claim is made
  that arbitrary concurrent external writes can always be preserved.
- `os.Root` confines actual target filesystem operations; symlink destinations
  are rejected. Version 2 staging lives in `data/export-publications/stages`,
  alongside its catalog and recovery blobs, so application-data backup includes
  the entire operation and export-parent write permission is unnecessary.
  Version 1 external-stage journals are blocked and preserved, not migrated or
  silently recovered. Target replacement still uses a synced temporary file in
  the destination directory, without requiring stage and target on one filesystem.
- Automated tests simulate process interruption and uncertain SQL commits.
  Actual NAS mount removal, filesystem power loss and appliance reboot remain
  deployment checks; there is no claim that those were tested here.
- Manual subtitle download/upload retain the existing link-before-SQL path and
  are not covered by the new pending protocol. Its file/registration crash
  window remains; durable recovery claims apply only to automatic subtitle tasks.
- The automatic subtitle file/SQL registration recovery is a separate implementation in
  `internal/subtitle`, not a guarantee conferred merely by durable task enqueue.
- Older binaries do not consume subtitle tasks or publication catalogs. Stop
  the app and retain matching database/files/catalog backups before downgrade.

## Validation

Using the verified Go 1.27.4 SDK:

- `go test -tags dev ./internal/app ./internal/library ./internal/library/scrape ./internal/tasks`: passed.
- `go test -race -tags dev ./internal/library/scrape ./internal/tasks ./internal/app`: passed.
- Receipt tests cover committed, proven uncommitted, missing and mismatched rows.
- Integration tests cover SQL failure rollback/retry, a commit that succeeded
  despite a returned error, startup recovery before a writer and blocking an
  unknown receipt. Status snapshots are asserted for blocked/recovered states.
- File-layer fault and confinement tests are in `internal/export/journal*_test.go`.
- `node --test web/tests/subtitle-status.node.mjs`: 4 passed.
- `git diff --check`: passed at this integration checkpoint.
- Full frontend and overall repository verification are coordinated separately.
  No FPK build, commit, push or release is performed by this change.

Suggested commit message: `Journal media publication and persist subtitle follow-ups`
