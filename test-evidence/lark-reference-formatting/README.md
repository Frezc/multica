# Lark reference formatting review evidence

Tested source: `034d2dd717c538f32bc3dee6d5286306ff61fe37` (tree `8e3b9deea7e793a8e1ea9da5a7c1912a2cb4f993`). Comparison: `b24f48309a7716df5850eab0912a510fd60e096c`. Public base: `d7d1831fcf39b874911aae1e48b2fbf804bd068e`.

## Checks

From the repository root, using Go 1.26.9:

```sh
go test -C server -race ./internal/integrations/channel ./internal/integrations/lark \
  -run "$(cat /path/to/focused-pattern.txt)" -count=1 -json
go vet -C server ./internal/integrations/channel ./internal/integrations/lark ./cmd/server
go test -C server ./cmd/server -run '^$' -count=1
git diff --check
```

411 selected leaf cases passed with the race detector; zero failures and skips. The reference-specific subset contains 255 leaf cases. New review regressions produced 11 failing leaf cases on the preceding resolver. The converter is unchanged in this revision, retaining Markdown labels, nested/reference-style links, container continuations, titles, encoded destinations, adjacent identifiers, GFM cells, URLs, code and image protections.

The committed tests include syntactically valid but nonexistent UUID targets for all five resource kinds and nonexistent matching issue identifiers; 1, 32 and 80 groups of six distinct references (up to 480 targets) each use one workspace query. Non-candidate or protected-only replies use zero. A failed workspace lookup is attempted once per reply. Workspace metadata refreshes on the next reply. UUID issue links work without an issue prefix; member links retain the mention's user ID. Unrelated UTF-8/SHA-256 stay unchanged for other workspace prefixes, while a workspace named UTF can still link UTF-8.

## Captured outbound payloads

`outbound-before.json` and `outbound-after.json` are actual Patcher output from identical synthetic input. A temporary test-only query wrapper returns the `demo` workspace and not-found for every target; a fake API transport captures `SendMarkdownCard`. Both revisions perform one workspace read. The preceding revision makes seven resource reads and leaves unresolved references; the candidate makes none and emits the workspace links. The expected direct-link assertion fails before and passes after. The temporary capture harness is not part of the source PR.

The capture includes an HTTPS link immediately followed by DEMO-1000: the destination is preserved and the adjacent identifier is linked. Inline code and unrelated UTF-8/SHA-256 are preserved; broadcast is display text.

## Screenshot and privacy scope

The screenshots render the measured local test summary and captured outbound Markdown. Runner/checkout identifiers are covered during rendering. Resource names, UUIDs and domains are synthetic; no credentials, private hosts, real recipients or service data are present. These are local automated test/payload screenshots, not a live Feishu client or a GitHub CI screenshot.

Local database/Redis integration suites, frontend suites and live Feishu/model round trips were not run. GitHub Actions results are linked separately from the PR description for the exact source head.
