package lark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type referenceLinkQueryFake struct {
	*fakePatcherQueries
	workspace      db.Workspace
	issue          db.Issue
	workspaceErr   error
	issueErr       error
	workspaceReads int
	idReads        []db.GetIssueInWorkspaceParams
	numberReads    []db.GetIssueByNumberParams
	referenceID    pgtype.UUID
	referenceWS    pgtype.UUID
	referenceErr   error
	referenceReads []referenceLookup
}

type referenceLookup struct {
	kind            string
	id, workspaceID pgtype.UUID
}

func (f *referenceLinkQueryFake) readReference(kind string, id, workspaceID pgtype.UUID) error {
	f.referenceReads = append(f.referenceReads, referenceLookup{kind, id, workspaceID})
	if f.referenceErr != nil {
		return f.referenceErr
	}
	if workspaceID != f.workspace.ID || id != f.referenceID {
		return pgx.ErrNoRows
	}
	return nil
}

func (f *referenceLinkQueryFake) GetProjectInWorkspace(_ context.Context, p db.GetProjectInWorkspaceParams) (db.Project, error) {
	return db.Project{ID: f.referenceID, WorkspaceID: f.referenceWS}, f.readReference("project", p.ID, p.WorkspaceID)
}
func (f *referenceLinkQueryFake) GetAgentInWorkspace(_ context.Context, p db.GetAgentInWorkspaceParams) (db.Agent, error) {
	return db.Agent{ID: f.referenceID, WorkspaceID: f.referenceWS}, f.readReference("agent", p.ID, p.WorkspaceID)
}
func (f *referenceLinkQueryFake) GetSquadInWorkspace(_ context.Context, p db.GetSquadInWorkspaceParams) (db.Squad, error) {
	return db.Squad{ID: f.referenceID, WorkspaceID: f.referenceWS}, f.readReference("squad", p.ID, p.WorkspaceID)
}
func (f *referenceLinkQueryFake) GetMemberByUserAndWorkspace(_ context.Context, p db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	return db.Member{ID: f.issue.ID, UserID: f.referenceID, WorkspaceID: f.referenceWS}, f.readReference("member", p.UserID, p.WorkspaceID)
}

func (f *referenceLinkQueryFake) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	f.workspaceReads++
	return f.workspace, f.workspaceErr
}

func (f *referenceLinkQueryFake) GetIssueInWorkspace(_ context.Context, p db.GetIssueInWorkspaceParams) (db.Issue, error) {
	f.idReads = append(f.idReads, p)
	if f.issueErr != nil {
		return db.Issue{}, f.issueErr
	}
	if p.WorkspaceID != f.workspace.ID || p.ID != f.issue.ID {
		return db.Issue{}, pgx.ErrNoRows
	}
	return f.issue, nil
}

func (f *referenceLinkQueryFake) GetIssueByNumber(_ context.Context, p db.GetIssueByNumberParams) (db.Issue, error) {
	f.numberReads = append(f.numberReads, p)
	if f.issueErr != nil {
		return db.Issue{}, f.issueErr
	}
	if p.WorkspaceID != f.workspace.ID || p.Number != f.issue.Number {
		return db.Issue{}, pgx.ErrNoRows
	}
	return f.issue, nil
}

func referenceLinkFixture(t *testing.T) (*Patcher, *referenceLinkQueryFake) {
	t.Helper()
	p, base, _ := newTestPatcher(t)
	wsID := uuidFromString(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
	q := &referenceLinkQueryFake{
		fakePatcherQueries: base,
		workspace:          db.Workspace{ID: wsID, Slug: "feedback", IssuePrefix: "FEED"},
		referenceID:        uuidFromString(t, "11111111-1111-4111-8111-111111111111"),
		referenceWS:        wsID,
		issue: db.Issue{
			ID:          uuidFromString(t, "44444444-4444-4444-8444-444444444444"),
			WorkspaceID: wsID, Number: 193,
		},
	}
	p.queries = q
	p.cfg.AppURL = "https://deployment.example/"
	return p, q
}

func TestLinkReplyReferencesEntities(t *testing.T) {
	for kind, route := range map[string]string{"project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			q.workspace.IssuePrefix = "" // Non-issue references do not need an issue prefix.
			id := uuidString(q.referenceID)
			body := "[@Label](mention://" + kind + "/" + id + ")"
			want := "[@Label](https://deployment.example/feedback/" + route + "/" + id + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body+" "+body); got != want+" "+want {
				t.Fatalf("reply=%q, want=%q", got, want+" "+want)
			}
			if q.workspaceReads != 1 || len(q.referenceReads) != 1 || q.referenceReads[0] != (referenceLookup{kind, q.referenceID, q.workspace.ID}) {
				t.Fatalf("unscoped or duplicate reads: %+v", q.referenceReads)
			}
		})
	}
}

func TestLinkReplyReferencesMarkdownForms(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		for _, tc := range []struct {
			name, body, want string
			protected        bool
		}{
			{"formatted label", `[**Label [inner]**](%s)`, `[**Label [inner]**](%s)`, false},
			{"escaped label and title", `[Label \[inner\]](<%s> "Title")`, `[Label \[inner\]](<%s> "Title")`, false},
			{"full reference", "[Label][ref]\n\n[ref]: %s", "[Label](%s)\n\n[ref]: %s", false},
			{"collapsed reference", "[Label][]\n\n[Label]: %s", "[Label](%s)\n\n[Label]: %s", false},
			{"shortcut reference", "[Label]\n\n[Label]: %s", "[Label](%s)\n\n[Label]: %s", false},
			{"multiline quote", "> [Label](\n> %s\n> )", "> [Label](\n> %s\n> )", false},
			{"quoted angle and title", "> [Label](\n> <%s> \"Title\"\n> )", "> [Label](\n> <%s> \"Title\"\n> )", false},
			{"nested quoted list", "> - [Label](\n>   %s\n>   )", "> - [Label](\n>   %s\n>   )", false},
			{"full reference with title", "[Label][ref]\n\n[ref]: %s \"Title\"", "[Label](%s \"Title\")\n\n[ref]: %s \"Title\"", false},
			{"collapsed reference with title", "[Label][]\n\n[Label]: %s \"Title\"", "[Label](%s \"Title\")\n\n[Label]: %s \"Title\"", false},
			{"shortcut reference with title", "[Label]\n\n[Label]: %s \"Title\"", "[Label](%s \"Title\")\n\n[Label]: %s \"Title\"", false},
			{"code", "`[Label](%s)`", "", true},
			{"image", "![Label](%s)", "", true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				p, q := referenceLinkFixture(t)
				id, resolved := uuidString(q.referenceID), uuidString(q.referenceID)
				if kind == "issue" {
					id, resolved = uuidString(q.issue.ID), "FEED-193"
				}
				target := "mention://" + kind + "/" + id
				body := fmt.Sprintf(tc.body, target)
				want := body
				if !tc.protected {
					link := "https://deployment.example/feedback/" + route + "/" + resolved
					if strings.Contains(tc.body, "\n\n") {
						want = fmt.Sprintf(tc.want, link, target)
					} else {
						want = fmt.Sprintf(tc.want, link)
					}
				}
				if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
					t.Fatalf("reply=%q, want=%q", got, want)
				}
				if tc.protected && q.workspaceReads != 0 {
					t.Fatal("protected Markdown caused a lookup")
				}
			})
		}
	}
}

func TestLinkReplyReferencesEncodedTargets(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			id, resolved := uuidString(q.referenceID), uuidString(q.referenceID)
			if kind == "issue" {
				id, resolved = uuidString(q.issue.ID), "FEED-193"
			}
			var body, want []string
			link := "https://deployment.example/feedback/" + route + "/" + resolved
			for _, scheme := range []string{`mention\://`, `mention:\/\/`, "mention&#58;//", "&#109;ention://", "mention&colon;&sol;&sol;"} {
				body = append(body, "[@Label]("+scheme+kind+"/"+id+")")
				want = append(want, "[@Label]("+link+")")
			}
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, strings.Join(body, " ")); got != strings.Join(want, " ") {
				t.Fatalf("reply=%q, want=%q", got, strings.Join(want, " "))
			}
			if q.workspaceReads != 1 || len(q.referenceReads)+len(q.idReads) != 1 {
				t.Fatal("normalized aliases did not share the resource cache")
			}
		})
	}
}

func TestLinkReplyReferencesPreservesUnresolved(t *testing.T) {
	for _, kind := range []string{"project", "member", "agent", "squad"} {
		for _, failure := range []string{"missing", "foreign workspace", "unknown UUID", "malformed UUID", "workspace error"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				p, q := referenceLinkFixture(t)
				id := uuidString(q.referenceID)
				switch failure {
				case "missing":
					q.referenceErr = pgx.ErrNoRows
				case "foreign workspace":
					q.referenceWS = q.issue.ID
				case "unknown UUID":
					id = uuidString(q.issue.ID)
				case "malformed UUID":
					id = "not-an-id"
				case "workspace error":
					q.workspaceErr = errors.New("unavailable")
				}
				body := "[@Label](mention://" + kind + "/" + id + ")"
				if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body {
					t.Fatalf("unresolved reference changed: %q", got)
				}
			})
		}
	}
}

func TestLinkReplyReferencesSharesBudgetAndCacheByKind(t *testing.T) {
	p, q := referenceLinkFixture(t)
	id := uuidString(q.referenceID)
	body := ""
	for _, kind := range []string{"project", "member", "agent", "squad"} {
		body += "[@Label](mention://" + kind + "/" + id + ") "
	}
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, strings.Repeat(body, 3)); strings.Contains(got, "mention://") || len(q.referenceReads) != 4 {
		t.Fatalf("cache mixed target kinds: reads=%+v, reply=%q", q.referenceReads, got)
	}
	p, q = referenceLinkFixture(t)
	body = ""
	for i := 1; i <= 30; i++ {
		if i%2 == 0 {
			body += fmt.Sprintf("FEED-%d ", i)
		} else {
			body += fmt.Sprintf("[Project](mention://project/%08x-1111-4111-8111-111111111111) ", i)
		}
	}
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body || len(q.referenceReads)+len(q.numberReads) != 20 {
		t.Fatalf("mixed budget: references=%d, issues=%d", len(q.referenceReads), len(q.numberReads))
	}
	p, q = referenceLinkFixture(t)
	body = "FEED-194 [Project](mention://project/" + uuidString(q.referenceID) + ")"
	want := "FEED-194 [Project](https://deployment.example/feedback/projects/" + uuidString(q.referenceID) + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
		t.Fatalf("issue miss suppressed later project: %q", got)
	}
}

func TestLinkReplyReferencesBroadcastIsDisplayOnly(t *testing.T) {
	p, q := referenceLinkFixture(t)
	q.workspaceErr = errors.New("no lookup needed")
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, "FYI [@all](mention://all/all)"); got != "FYI @all" || q.workspaceReads != 0 {
		t.Fatalf("broadcast conversion=%q, reads=%d", got, q.workspaceReads)
	}
}

func TestPatcherLinksAllReferencesForFeishu(t *testing.T) {
	p, base, api := groupPatcherWithSender(t, "ou_sender")
	_, q := referenceLinkFixture(t)
	q.fakePatcherQueries = base
	base.installation.WorkspaceID = q.workspace.ID
	p.queries = q
	p.cfg.AppURL = "https://deployment.example"
	id := uuidString(q.referenceID)
	body := "[Issue](mention://issue/" + uuidString(q.issue.ID) + ")"
	want := "[Issue](https://deployment.example/feedback/issues/FEED-193)"
	for _, entry := range []struct{ kind, route string }{{"project", "projects"}, {"member", "members"}, {"agent", "agents"}, {"squad", "squads"}} {
		body += " [@Label](mention://" + entry.kind + "/" + id + ")"
		want += " [@Label](https://deployment.example/feedback/" + entry.route + "/" + id + ")"
	}
	body += " [@all](mention://all/all)"
	want += " @all"
	p.handleEvent(events.Event{Type: protocol.EventChatDone, TaskID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", ChatSessionID: uuidString(base.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: body}})
	if len(api.mdCardSent) != 1 || len(api.textSent) != 0 {
		t.Fatalf("expected one reference card: cards=%d, texts=%d", len(api.mdCardSent), len(api.textSent))
	}
	if got := api.mdCardSent[0]; got.Markdown != prependMarkdownMention("ou_sender", want) || got.ReplyTarget != threadReplyTarget(base.binding) {
		t.Fatalf("outbound reference conversion or routing changed: %+v", got)
	}
}

func TestLinkReplyIssues(t *testing.T) {
	const (
		internal = "mention://issue/44444444-4444-4444-8444-444444444444"
		web      = "https://deployment.example/feedback/issues/FEED-193"
	)
	for _, tc := range []struct{ name, body, want string }{
		{"UUID link", "已记录：[FEED-193](" + internal + ")。", "已记录：[FEED-193](" + web + ")。"},
		{"identifier link", "[反馈](mention://issue/FEED-193)", "[反馈](" + web + ")"},
		{"nested label", "[反馈 [详情]](" + internal + ")", "[反馈 [详情]](" + web + ")"},
		{"escaped label", `[反馈 \[详情\]](` + internal + `)`, `[反馈 \[详情\]](` + web + `)`},
		{"code in label", "[详情 `]` FEED-193](" + internal + ")", "[详情 `]` FEED-193](" + web + ")"},
		{"title and angle destination", "[反馈](<" + internal + "> \"详情\")", "[反馈](<" + web + "> \"详情\")"},
		{"internal full reference", "[反馈][task]\n\n[task]: " + internal, "[反馈](" + web + ")\n\n[task]: " + internal},
		{"internal collapsed reference", "[FEED-193][]\n\n[FEED-193]: " + internal, "[FEED-193](" + web + ")\n\n[FEED-193]: " + internal},
		{"internal shortcut reference", "[FEED-193]\n\n[FEED-193]: " + internal, "[FEED-193](" + web + ")\n\n[FEED-193]: " + internal},
		{"bare and punctuation", "已记录：FEED-193。 FEED-193.", "已记录：[FEED-193](" + web + ")。 [FEED-193](" + web + ")."},
		{"emphasis", "**FEED-193**", "**[FEED-193](" + web + ")**"},
		{"inline code", "`FEED-193` `[反馈](" + internal + ")`", ""},
		{"fenced code", "```md\nFEED-193\n[反馈](" + internal + ")\n```", ""},
		{"indented code", "    FEED-193\n    [反馈](" + internal + ")\n", ""},
		{"existing link", "[FEED-193](https://elsewhere.example)", ""},
		{"existing image", "![FEED-193](https://elsewhere.example/image.png)", ""},
		{"reference link", "[FEED-193][docs]\n\n[docs]: https://elsewhere.example", ""},
		{"raw URL query", "https://elsewhere.example?q=FEED-193#FEED-193", ""},
		{"URL parentheses", "https://elsewhere.example?q=(FEED-193)", ""},
		{"URL angle brackets", "<https://elsewhere.example/FEED-193>", ""},
		{"email", "person+FEED-193@example.test", ""},
		{"relative path", "/path?q=FEED-193", ""},
		{"file and path tokens", "FEED-193.ts path/FEED-193 FEED-193/file", ""},
		{"escaped example", `\[FEED-193](` + internal + `)`, ""},
		{"other prefix", "OTHER-193", ""},
		{"embedded identifier", "xFEED-193 FEED-193x FEED-193-2", ""},
		{"unknown issue", "FEED-194", ""},
		{"noncanonical number", "FEED-0193 FEED-0 FEED-99999999999999999999", ""},
		{"plain reply", "已记录。", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			want := tc.want
			if want == "" {
				want = tc.body
			}
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, tc.body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
		})
	}
}

func TestLinkReplyIssuesAcceptsNumericPrefixes(t *testing.T) {
	for _, prefix := range []string{"1", "123", "1A", "A1", "1234567890"} {
		t.Run(prefix, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			q.workspace.IssuePrefix = prefix
			identifier := prefix + "-193"
			link := "https://deployment.example/feedback/issues/" + identifier
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, identifier); got != "["+identifier+"]("+link+")" {
				t.Fatalf("bare identifier did not link: %q", got)
			}
			q.numberReads = nil
			body := identifier + " [编号](mention://issue/" + identifier + ") [UUID](mention://issue/44444444-4444-4444-8444-444444444444)"
			want := "[" + identifier + "](" + link + ") [编号](" + link + ") [UUID](" + link + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
			if len(q.numberReads) != 1 || len(q.idReads) != 1 {
				t.Fatalf("unexpected lookups: numbers=%d, UUIDs=%d", len(q.numberReads), len(q.idReads))
			}
		})
	}
}

func TestLinkReplyIssuesPreservesURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.test/path：FEED-193",
		"https://example.test/?q=中文，FEED-193",
		"https://example.test/#上下文；FEED-193",
		"https://example.test/?q=中文。FEED-193",
		"https://example.test/?q=中文：(**FEED-193**)",
		"https://example.test/?q=中文：FEED-193&amp;other=FEED-193",
		"HTTP://localhost/?q=中文：FEED-193",
		"ftp://example.test/中文：FEED-193",
		"www.example.test/?q=中文：FEED-193",
		"//example.test/?q=中文：FEED-193",
		"/path?q=中文：FEED-193",
		`C:\path\中文：FEED-193`,
		"mailto:person@example.test?subject=中文：FEED-193",
		"person+FEED-193+tag@example.test",
		"FEED-193+person@example.test",
		"https://example.test/?q=" + strings.Repeat("中文；FEED-193", 30),
	} {
		t.Run(rawURL, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, rawURL); got != rawURL || q.workspaceReads != 0 {
				t.Fatalf("URL-only reply changed or queried workspace: reply=%q, reads=%d", got, q.workspaceReads)
			}
			body := rawURL + "\u00a0正文：FEED-193。"
			want := rawURL + "\u00a0正文：[FEED-193](https://deployment.example/feedback/issues/FEED-193)。"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
			if len(q.numberReads) != 1 {
				t.Fatalf("URL references consumed lookups: %+v", q.numberReads)
			}
		})
	}
}

func TestLinkReplyIssuesPreservesReplyWhenLookupUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Patcher, *referenceLinkQueryFake)
	}{
		{"workspace error", func(_ *Patcher, q *referenceLinkQueryFake) { q.workspaceErr = errors.New("unavailable") }},
		{"issue error", func(_ *Patcher, q *referenceLinkQueryFake) { q.issueErr = errors.New("unavailable") }},
		{"missing slug", func(_ *Patcher, q *referenceLinkQueryFake) { q.workspace.Slug = "" }},
		{"missing prefix", func(_ *Patcher, q *referenceLinkQueryFake) { q.workspace.IssuePrefix = "" }},
		{"missing app URL", func(p *Patcher, _ *referenceLinkQueryFake) { p.cfg.AppURL = "" }},
		{"non HTTP app URL", func(p *Patcher, _ *referenceLinkQueryFake) { p.cfg.AppURL = "mention://app" }},
		{"foreign issue", func(_ *Patcher, q *referenceLinkQueryFake) {
			q.issue.WorkspaceID = uuidFromString(t, "55555555-5555-4555-8555-555555555555")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			tc.setup(p, q)
			body := "已记录：FEED-193 [任务](mention://issue/44444444-4444-4444-8444-444444444444)"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body {
				t.Fatalf("reply changed on failed enrichment: %q", got)
			}
		})
	}
}

func TestLinkReplyIssuesBoundsAndScopesLookups(t *testing.T) {
	p, q := referenceLinkFixture(t)
	body := strings.Repeat("FEED-193 ", 30)
	got := p.linkReplyReferences(context.Background(), q.workspace.ID, body)
	if len(q.numberReads) != 1 || q.numberReads[0].WorkspaceID != q.workspace.ID || strings.Count(got, "https://") != 30 {
		t.Fatalf("repeated references: lookups=%+v, reply=%q", q.numberReads, got)
	}
	p, q = referenceLinkFixture(t)
	body = ""
	for i := 1; i <= 30; i++ {
		body += fmt.Sprintf("FEED-%d ", i)
	}
	if got = p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body || len(q.numberReads) != 20 {
		t.Fatalf("lookup bound: lookups=%d, reply=%q", len(q.numberReads), got)
	}
	for _, read := range q.numberReads {
		if read.WorkspaceID != q.workspace.ID {
			t.Fatalf("unscoped lookup: %+v", read)
		}
	}
	p, q = referenceLinkFixture(t)
	body = "[任务](mention://issue/44444444-4444-4444-8444-444444444444)"
	_ = p.linkReplyReferences(context.Background(), q.workspace.ID, body)
	if len(q.idReads) != 1 || q.idReads[0].WorkspaceID != q.workspace.ID {
		t.Fatalf("unscoped UUID lookup: %+v", q.idReads)
	}
}

func TestPatcherLinksIssueReferencesForFeishu(t *testing.T) {
	for _, body := range []string{
		"已记录：[DEMO-193](mention://issue/44444444-4444-4444-8444-444444444444)。",
		"已记录：DEMO-193。",
	} {
		t.Run(body, func(t *testing.T) {
			p, base, api := groupPatcherWithSender(t, "ou_sender")
			wsID := uuidFromString(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
			base.installation.WorkspaceID = wsID
			p.queries = &referenceLinkQueryFake{
				fakePatcherQueries: base,
				workspace:          db.Workspace{ID: wsID, Slug: "demo", IssuePrefix: "DEMO"},
				issue: db.Issue{
					ID:          uuidFromString(t, "44444444-4444-4444-8444-444444444444"),
					WorkspaceID: wsID, Number: 193,
				},
			}
			// Set through the config exactly as the production router does.
			p.cfg.AppURL = "https://multica.example"
			p.handleEvent(events.Event{
				Type:   protocol.EventChatDone,
				TaskID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", ChatSessionID: uuidString(base.binding.ChatSessionID),
				Payload: protocol.ChatDonePayload{Content: body},
			})
			if len(api.mdCardSent) != 1 || len(api.textSent) != 0 {
				t.Fatalf("expected one linked markdown reply, got cards=%d text=%d", len(api.mdCardSent), len(api.textSent))
			}
			got := api.mdCardSent[0]
			want := prependMarkdownMention("ou_sender", "已记录：[DEMO-193](https://multica.example/demo/issues/DEMO-193)。")
			if got.Markdown != want {
				t.Fatalf("reply = %q, want %q", got.Markdown, want)
			}
			if got.ReplyTarget != threadReplyTarget(base.binding) {
				t.Fatalf("reply target changed: %+v", got.ReplyTarget)
			}
		})
	}
}
