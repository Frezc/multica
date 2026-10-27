package lark

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type referenceLinkQueries interface {
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	GetIssueInWorkspace(context.Context, db.GetIssueInWorkspaceParams) (db.Issue, error)
	GetIssueByNumber(context.Context, db.GetIssueByNumberParams) (db.Issue, error)
	GetProjectInWorkspace(context.Context, db.GetProjectInWorkspaceParams) (db.Project, error)
	GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error)
	GetSquadInWorkspace(context.Context, db.GetSquadInWorkspaceParams) (db.Squad, error)
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

// linkReplyReferences changes only the outbound copy. Resolution is scoped to
// the originating workspace and shares one bounded enrichment budget.
func (p *Patcher) linkReplyReferences(parent context.Context, workspaceID pgtype.UUID, content string) string {
	appURL, err := url.Parse(p.cfg.AppURL)
	if err != nil || (appURL.Scheme != "http" && appURL.Scheme != "https") || appURL.Host == "" || !workspaceID.Valid {
		return content
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	var ws db.Workspace
	var workspaceErr error
	loaded := false
	type target struct{ kind, reference string }
	links := make(map[target]string)
	lookups := 0
	return channel.LinkReferences(content, func(kind, reference string) string {
		key := target{kind, reference}
		if link, ok := links[key]; ok {
			return link
		}
		links[key] = ""
		switch kind {
		case "issue", "project", "agent", "squad", "member":
		default:
			return ""
		}
		id, parseErr := util.ParseUUID(reference)
		if kind != "issue" && (parseErr != nil || !id.Valid) {
			return ""
		}
		if !loaded {
			loaded = true
			ws, workspaceErr = p.queries.GetWorkspace(ctx, workspaceID)
		}
		if workspaceErr != nil || ws.ID != workspaceID || ws.Slug == "" || lookups >= 20 || ctx.Err() != nil {
			return ""
		}
		var resolved string
		switch kind {
		case "issue":
			if ws.IssuePrefix == "" {
				return ""
			}
			var issue db.Issue
			var err error
			if parseErr == nil && id.Valid {
				lookups++
				issue, err = p.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
			} else {
				number, numberErr := strconv.ParseInt(strings.TrimPrefix(reference, ws.IssuePrefix+"-"), 10, 32)
				if numberErr != nil || number <= 0 || fmt.Sprintf("%s-%d", ws.IssuePrefix, number) != reference {
					return ""
				}
				lookups++
				issue, err = p.queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{WorkspaceID: workspaceID, Number: int32(number)})
			}
			if err != nil || issue.WorkspaceID != workspaceID || issue.Number <= 0 {
				return ""
			}
			resolved = fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
		case "project":
			lookups++
			project, lookupErr := p.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
			if lookupErr != nil || project.ID != id || project.WorkspaceID != workspaceID {
				return ""
			}
			resolved = util.UUIDToString(project.ID)
		case "agent":
			lookups++
			agent, lookupErr := p.queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
			if lookupErr != nil || agent.ID != id || agent.WorkspaceID != workspaceID {
				return ""
			}
			resolved = util.UUIDToString(agent.ID)
		case "squad":
			lookups++
			squad, lookupErr := p.queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: id, WorkspaceID: workspaceID})
			if lookupErr != nil || squad.ID != id || squad.WorkspaceID != workspaceID {
				return ""
			}
			resolved = util.UUIDToString(squad.ID)
		case "member":
			lookups++
			member, lookupErr := p.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: id, WorkspaceID: workspaceID})
			if lookupErr != nil || member.UserID != id || member.WorkspaceID != workspaceID {
				return ""
			}
			resolved = util.UUIDToString(member.UserID)
		}
		links[key] = channel.ReferenceWebLink(p.cfg.AppURL, ws.Slug, kind, resolved)
		return links[key]
	})
}
