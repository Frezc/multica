# Lark reference formatting review evidence

Tested source: `f950a748d82484914e8a78e4f62b7f947f8da505` (tree `9f3599326714980847ea4168c748b9c47a36b220`). Comparison: `b24f48309a7716df5850eab0912a510fd60e096c`. Public base: `d7d1831fcf39b874911aae1e48b2fbf804bd068e`.

## Checks

From the repository root: the race tests use Go 1.26.9; the final vet and vulnerability scan use the CI toolchain Go 1.27.3. The server compilation check preceded the dependency-only follow-up; the final CI compiles the updated module graph.

```sh
go test -C server -race ./internal/integrations/channel ./internal/integrations/lark ./internal/util \
  -run "$(cat /path/to/focused-pattern.txt)" -count=1 -json
GOTOOLCHAIN=go1.27.3 go vet -C server ./internal/integrations/channel ./internal/integrations/lark ./internal/util ./cmd/server
go test -C server ./cmd/server -run '^$' -count=1
git diff --check
```

522 selected leaf cases passed with the race detector; zero failures and skips. The reference-specific subset contains 255 leaf cases. The existing 411-case conversion/outbound set was rerun, together with 111 additional mention/HTML-label cases. New review regressions produced 11 failing leaf cases on the preceding resolver. The converter is unchanged in this revision, retaining Markdown labels, nested/reference-style links, container continuations, titles, encoded destinations, adjacent identifiers, GFM cells, URLs, code and image protections.

The committed tests include syntactically valid but nonexistent UUID targets for all five resource kinds and nonexistent matching issue identifiers; 1, 32 and 80 groups of six distinct references (up to 480 targets) each use one workspace query. Non-candidate or protected-only replies use zero. A failed workspace lookup is attempted once per reply. Workspace metadata refreshes on the next reply. UUID issue links work without an issue prefix; member links retain the mention's user ID. Unrelated UTF-8/SHA-256 stay unchanged for other workspace prefixes, while a workspace named UTF can still link UTF-8.

## CI dependency follow-up

The first CI run passed backend integration, agent, Windows, script and frontend aggregate checks, but `govulncheck` detected GO-2026-6860 in the existing golang.org/x/net v0.60.0 HTML parser. The follow-up upgrades x/net to v0.62.0 and the versions required by its module graph. No vulnerability check was disabled. Using the CI toolchain, `GOTOOLCHAIN=go1.27.3 go -C server tool govulncheck ./...` reports no vulnerabilities. A scan with the older local Go 1.26.9 toolchain separately detected the standard-library advisory GO-2026-6809; the CI toolchain is Go 1.27.3.

## Captured outbound payloads

`outbound-before.json` and `outbound-after.json` are actual Patcher output from identical synthetic input. A temporary test-only query wrapper returns the `demo` workspace and not-found for every target; a fake API transport captures `SendMarkdownCard`. Both revisions perform one workspace read. The preceding revision makes seven resource reads and leaves unresolved references; the candidate makes none and emits the workspace links. The expected direct-link assertion fails before and passes after. The temporary capture harness is not part of the source PR.

The capture includes an HTTPS link immediately followed by DEMO-1000: the destination is preserved and the adjacent identifier is linked. Inline code and unrelated UTF-8/SHA-256 are preserved; broadcast is display text.

## Screenshot and privacy scope

The screenshots render the measured local test summary and captured outbound Markdown. Runner/checkout identifiers are covered during rendering. Resource names, UUIDs and domains are synthetic; no credentials, private hosts, real recipients or service data are present. These are local automated test/payload screenshots, not a live Feishu client or a GitHub CI screenshot.

Local database/Redis integration suites, frontend suites and live Feishu/model round trips were not run. GitHub Actions results are linked separately from the PR description for the exact source head.
