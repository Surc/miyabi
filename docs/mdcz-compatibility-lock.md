# MDCz compatibility lock and attempt guards

The editable compatibility record is `extensions/mdcz-bridge/source-lock.json`.
The lock now pins MDCz **0.22.0** at `e869dc74e44d7e21d5ad5ed33ad9e40e5abed9be`
after isolated real-runtime validation. It does not merge Miyabi upstream.

After an intentionally reviewed lock change, run:

```sh
node extensions/mdcz-bridge/generate-compatibility.mjs
node extensions/mdcz-bridge/generate-compatibility.mjs --check
```

The checked-in generated outputs serve the Go consumer, bundled bridge and native
package manager. Changing a lock is not evidence of compatibility: the real pinned
runtime tests and artifact checks must pass before accepting another engine.
Bridge `1.1.0` advertises the `attempt-snapshot` capability. A new consumer must
verify the bridge version, exact engine revision, protocol, configuration schema
and required capabilities, not merely protocol 1.

## Request guard

A snapshot-aware operation sends both headers:

- `X-Miyabi-MDCz-Configuration-ID`: the configuration identity from health.
- `X-Miyabi-MDCz-Engine-Revision`: the validated engine revision.

Metadata, image and poster requests validate these under the same single-operation
slot as configuration writes. A missing half, changed profile, or changed engine
returns HTTP 409 **before** the adapter executes. The Go consumer must surface this
as a configuration conflict, not try another image and silently mix profiles.
Legacy clients omitting both headers remain accepted by the bridge; the new Go
consumer checks the advertised snapshot capability and does not assume an old
bridge implements this guard. Health and configuration reads remain available
while a job is active.

## Build provenance

Docker and native build recipes obtain the engine commit from the lock. The native
release recipe first checks generated files for drift. Staging verifies the actual
MDCz checkout and generated bridge constants, then emits `bridge-manifest.json`
with the complete compatibility tuple, build Node version and SHA-256 of
`bridge.js`. Native preparation compares it against the lock and recomputes that
hash before writing any package. `bundle.json` retains these fields along with the
existing package file hashes. These checks are provenance/integrity checks, not a
replacement for native-library and actual-device acceptance.

## Validation for this change

- `node --test extensions/mdcz-bridge/*.test.mjs`: 15 passed.
- `python3 -m unittest discover -s packaging/fnos/tests -p '*_test.py'`: 5 passed.
- Generated compatibility check and `git diff --check`: passed.
- Real MDCz 0.22 TypeScript no-emit, four runtime tests and Vite test compilation
  passed against its unchanged frozen lock. Docker/FPK builds, J4125 and real-site
  checks were not run. The user requested no packaging at this stage.

The HTTP tests cover changed configuration and engine rejection for metadata,
images and posters, missing header rejection, old-client compatibility, runtime
non-invocation on conflict, capability advertisement and generated-file drift.
The Python artifact tests reject stale compatibility tuples and changed bridge
bytes without building or staging an FPK.

## 旧独立 bridge 的升级边界

新 Go 消费者要求 `attempt-snapshot` 能力和锁中指定的 bridge 版本；旧独立 bridge 即使 MDCz 引擎 SHA 相同也不会被当作支持配置快照。应配套更新 bridge 后再启用增强。检查失败时主应用和原版设置仍可使用，不自动切换引擎，也不删除旧连接配置。


## MDCz 0.22 adapter migration

Only `adapter.ts` changes for upstream runtime signatures. Filename parsing now
passes `numberMappings` inside the rule object while retaining configured marker
rules. Poster badges pass their position inside the options object, preventing an
unintended top-left default. Old nonempty `personSync.actorAliases` maps are cloned
and converted to `{ name, aliases }` groups before upstream schema validation. The
original profile file/object is not rewritten; invalid and conflicting aliases
remain explicit validation errors.

Validation used Node 24.22.1 and the upstream-declared pnpm 10.28.2. Install ran with
`--frozen-lockfile`; the upstream package manifest and lock remained unchanged.
The bridge test workspace links only `sharp` to the server's frozen dependencies
for TypeScript/Vitest resolution (the production recipe externalizes it). Set a
writable `XDG_CACHE_HOME` for fontconfig during pixel tests.

The four runtime tests cover configuration persistence/masking plus nonempty old
alias conversion, source object preservation and conflict rejection; actual
filename mapping/part detection; JPEG pixels in all four badge positions, no-badge
original bytes, repeat generation and cancellation cleanup; actual local HTTP
through impit and sharp; and real aggregation with controlled crawler results.
Only the crawler's external source responses are mocked. This does **not** prove
real sites, Chinese font availability on J4125, NAS ACLs or FPK startup.

No change to the MDCz upstream checkout was committed. Build/test outputs remained
in the isolated validation checkout and were not copied into the Miyabi source.
