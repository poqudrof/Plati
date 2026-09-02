# Test Plan: Repo Files Copied into the Instance

**Goal:** Verify that files from `config/repos/` are correctly bind-mounted and copied to each
repo's `dest` when an instance is created with a template that uses `repos:`.

**Background:** `attachReposDirAndBuildCmds` binds the host `reposDir` into the instance at `/plati-repos`,
then prepends `cp` commands to `firstInitCmds`. It looks up each repo by name in the `git_repos` DB table;
if the record is missing or not `ready`, it falls back to checking the filesystem directly (as of the fix
in `instance_service.go`).

---

## 1. Go unit tests — `attachReposDirAndBuildCmds`

**File:** `backend/internal/services/instance_service_test.go` (new)

Uses an in-memory SQLite DB + a temp directory. No HTTP harness needed — call the method directly via a
minimal `InstanceService` constructed with a mock `IncusClient`.

| Case | DB row | Dir on disk | Expected commands |
|------|--------|-------------|-------------------|
| DB record `ready` + dir exists | `clone_status=ready`, `ssh_url=git@…` | `reposDir/myrepo/` | `cp -rp /plati-repos/myrepo /dest` + `cd /dest && git remote set-url origin git@…` |
| No DB record, dir exists | — | `reposDir/myrepo/` | `cp -rp /plati-repos/myrepo /dest` only (no git remote cmd) |
| No DB record, dir missing | — | — | `nil` (no commands) |
| DB record `clone_status=cloning`, dir exists | `clone_status=cloning` | `reposDir/myrepo/` | Falls back to filesystem: `cp -rp …` only |
| `reposDir` is empty string | any | any | `nil` immediately, no `AttachHostPath` call |

Each case must also assert that the mock's `AttachHostPath` was called with the correct host path (or not
called when returning early). The mock needs an `attachedHostPaths []string` field added.

---

## 2. Go integration tests — full create flow

**File:** `backend/internal/integration/workflow_test.go` (extend)

### Mock changes required

- Add `attachedHostPaths []string` to `mockIncusClient`; update `AttachHostPath` stub to `append` the `hostPath`.
- Add `runCommands [][]string` to capture every `RunCommand` call.

### `TestReposCopiedOnCreate` — filesystem fallback

1. Create a temp `reposDir` containing `myrepo/` with a dummy file (`hello.txt`). No DB row for the repo.
2. Construct harness with `reposDir` set to that temp dir.
3. Seed DB: insert template with `repos: [{"name":"myrepo","dest":"/home/ubuntu/myrepo"}]`.
4. `POST /api/v1/instances` with that template ID; wait for `status=running`.
5. Assert:
   - `h.mock.attachedHostPaths` contains `reposDir`.
   - `h.mock.runCommands` contains a command slice matching `["bash","-c","cp -rp /plati-repos/myrepo /home/ubuntu/myrepo"]`.
   - `h.mock.runCommands` does **not** contain any `git remote set-url` command.

### `TestReposCopiedOnCreate_WithDBRecord` — DB-driven path

Same as above, but also seed `git_repos` with `name=myrepo, ssh_url=git@github.com:org/myrepo.git, clone_status=ready`.

5. Assert:
   - `h.mock.runCommands` contains `cp -rp /plati-repos/myrepo /home/ubuntu/myrepo`.
   - `h.mock.runCommands` contains `cd /home/ubuntu/myrepo && git remote set-url origin git@github.com:org/myrepo.git`.

### `TestReposSkippedWhenDirMissing`

Template has `repos: [{"name":"ghost","dest":"/home/ubuntu/ghost"}]`. No DB row. No dir on disk.

5. Assert:
   - `h.mock.runCommands` contains no `cp` or `git remote` commands.
   - Instance still reaches `running` (creation does not fail).

### `TestReposNotAttachedWhenReposDirEmpty`

Harness constructed with `reposDir=""`.

5. Assert:
   - `h.mock.attachedHostPaths` is empty.
   - No `cp` command in `h.mock.runCommands`.

---

## 3. Shell integration test — real Incus verification

**File:** `scripts/test-api-repos.sh` (new, mirrors `test-api-lifecycle.sh` structure)

**Requirements:** running backend + Incus server + `config/repos/AI-state-art-public/` present on disk.
Run tier: `--repos` flag added to `tests.sh` (or included in `--all`).

### Pre-cleanup (top of script)

Delete any leftover instance whose name starts with `repo-test-` so re-runs don't fail on uniqueness.

### Steps

```
1.  Health check  GET /health → status=ok
2.  Admin login   POST /auth/login
3.  Find site-ia-gen template
      GET /api/v1/templates → filter by slug="site-ia-gen"
      fail if not found
4.  Create instance
      POST /api/v1/instances  {"name":"repo-test-<timestamp>","template_id":<id>}
      capture id + incus_name
5.  Poll until running
      GET /api/v1/instances/<id> every 5s, up to 120s → status="running"
      fail on timeout
6.  Verify the copied repo via Incus CLI
      incus exec <incus_name> -- ls /home/ubuntu/AI-state-art-public
        pass if exit 0
      incus exec <incus_name> -- test -f /home/ubuntu/AI-state-art-public/package.json
        pass if exit 0
      incus exec <incus_name> -- test -f /home/ubuntu/AI-state-art-public/README.md
        pass if exit 0
      incus exec <incus_name> -- test -d /home/ubuntu/AI-state-art-public/app
        pass if exit 0
7.  Cleanup
      DELETE /api/v1/instances/<id>
        pass if HTTP 204
```

---

## Prerequisites / notes

- `newHarness` in `workflow_test.go` needs a way to inject a custom `reposDir` (e.g. a functional option
  or a direct field on `harness`). Currently `reposDir` is hardcoded to `""`.
- The shell test requires the `incus` CLI to be available on the test host; skip gracefully if not
  (`command -v incus` check, same pattern as existing step 9 in `test-api-lifecycle.sh`).
- After a Rebuild, verify the same files are still present (the `rebuild_commands` in `site-ia-gen.yaml`
  run `git pull --ff-only` — but since the dir is not a real git repo, that will `|| true` and not fail).
