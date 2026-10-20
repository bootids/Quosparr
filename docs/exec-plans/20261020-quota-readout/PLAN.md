---
id: 20261020-quota-readout
title: Versioned read-only quota interface and dispatch skill migration
status: done
kind: plan
created: 2026-10-20
updated: 2026-10-20
parent: 20261018-meter-providers
---
# Quota Readout

## Outcome and acceptance

Expose a versioned, credential-free quota report through the existing opt-in
status export and a headless `--quota` command. Migrate the quota reader and
model-selection consumer in the user's skills repository. Unknown, failed,
expired-window and old data must not become healthy dispatch evidence.
No HTTP server, new dependency, credential mutation, or on-demand refresh.

## Context and constraints

- `internal/runway/export.go` owns the opt-in `~/.quosparr/status.json` export.
- `internal/meter` already normalizes numeric pools and prepaid balances.
- `main.go` routes headless commands before desktop startup and locks.
- The external consumer is `agent-dispatch/scripts/{quota,select_model}.py`
  in the user's skills repository. Its unrelated untracked HANDOFF is preserved.
- Preserve export opt-in, current-account scope, provider demand and proxy policy.
- Knowledge lookup: reused the scheduling/pace boundary in the vault note
  “omp 多模型派发基线”; quota search returned no dedicated interface note.

## Milestones

1. Add report types, normalization, timestamps/status handling and CLI query.
2. Connect report production without parsing localized quota/balance text.
3. Migrate skill/scripts and add focused regression tests plus a CLI-to-reader check.

## Decisions

Use `status.json.quotaReport` with its own schema version. Query is offline and
does not start the app. The caller chooses a maximum data age (default 900s).
Refresh remains the application's responsibility; hidden providers still need
to be selected in the panel/status/floating settings to be polled. Unsupported
providers (including MiMo) do not fall back to another quota source.

## Current state

Implemented and documented in `docs/quota-readout.md`. The consumer scripts,
skill entrypoint and routing reference in the skills repository now use only
Quosparr's versioned readout. On the user's explicit follow-up request, the
installed macOS app was backed up, replaced and restarted; existing settings
and accounts were retained without manual edits.
Provider callbacks update the opt-in export on manual refresh/account change;
the report is independent of Codex's model-window visibility preference.

## Validation evidence

- passed: repository root, `go test ./...`; all packages passed. After the final
  API-mode guard change, `go test ./internal/runway` passed again.
- passed: repository root, `go vet ./internal/export ./internal/runway ./internal/app .`.
- passed: repository root, `go build -o out/scratch/quosparr .`.
- passed: repository root, `go test -race ./internal/runway -run
  'TestQuotaReadout|TestStatusJSONMeterProviderRoundTrip' -count=1` (before final
  API-mode guard; provider callback implementation unchanged afterwards).
- passed: skills repository `agent-dispatch/scripts`,
  `QUOSPARR_TEST_BIN=<built executable> python3 -B -m unittest test_quota test_select_model`;
  45 tests, no skips. The CLI integration uses a temporary HOME with synthetic
  status JSON, verifies stale rejection/balances and file non-mutation, and
  passes the result through the actual quota reader and model allowance logic.
- passed: `git diff --check` in both repositories.
- blocked: skill-creator `quick_validate.py` cannot import `yaml` (PyYAML absent).
  No dependency was installed; frontmatter was not changed.
- passed (follow-up, macOS arm64): repository root,
  `V1_DEVELOPMENT_PACKAGE=1 V1_BUILD_DIR=<local build directory> bash Scripts/package-v1-app.sh`.
  Host, Widget, bridge, SQLite, stable signing requirements and final signatures
  verified. Evidence: `out/scratch/quota-local-package.log`; build and ZIP under
  `out/20261020-quota-readout/local-update/`.
- passed (follow-up): stopped old installed PID 77253 with the application quit
  event; moved the unmodified old app to
  `out/20261020-quota-readout/backup-20261020-131501/Quosparr.app`, installed the
  verified bundle in `/Applications/Quosparr.app`, and launched it without
  `--settings`. New PID 84476 holds the normal application lock. Installed and
  built executable SHA-256 matched; installed deep/strict signature check passed.
- passed (follow-up): the installed app's `--quota codex kimi zai` produced
  schema 1 from a newly generated status export. The actual skills quota reader
  discovered the installed app without overrides and returned Kimi/Z.ai `ok`
  (ages 4s/3s), Codex `not_configured`; no account configuration was changed.
- passed (after startup refresh): the installed app and actual skills reader
  returned Codex `ok`; `select_model.allowances({"codex"})` contained Codex with
  no errors. The initial `not_configured` observation was a startup transient,
  not missing configuration; no credential/configuration changes were needed.
- not run: full live provider coverage, update/upgrade-feed acceptance or other
  platforms' native acceptance. The installed local development package has
  automatic updates disabled, as required by the development packaging policy.

## Outcome and remaining gaps

Source/consumer integration and the requested local replacement/restart are
complete. The existing export preference was already enabled and the live
Codex/Kimi/Z.ai readout was verified. Querying an old/missing report is an explicit error. Interface is
offline-only; no promise of forced refresh or background app startup.

Knowledge reuse/writeback: updated the existing vault scheduling concept
“omp 多模型派发基线” with evidence freshness and model-group boundaries,
explicitly distinguished from real-service validation. No tool/source bundle
was copied to the vault.
