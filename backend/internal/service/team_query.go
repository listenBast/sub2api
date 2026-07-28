package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbteam "github.com/Wei-Shaw/sub2api/ent/team"
	dbmembership "github.com/Wei-Shaw/sub2api/ent/teammembership"
	dbtransaction "github.com/Wei-Shaw/sub2api/ent/teamtransaction"
	dbusagelog "github.com/Wei-Shaw/sub2api/ent/usagelog"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/lib/pq"
)

func (s *TeamService) GetContext(ctx context.Context, userID int64) (*TeamContext, error) {
	owned, err := s.client.Team.Query().
		Where(dbteam.OwnerIDEQ(userID)).
		WithOwner().
		WithMemberships(func(query *dbent.TeamMembershipQuery) { query.WithUser() }).
		Only(ctx)
	if err == nil {
		summary := teamSummaryFromEntity(owned)
		return &TeamContext{Role: TeamRoleOwner, Team: &summary}, nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}

	membership, err := s.client.TeamMembership.Query().
		Where(dbmembership.UserIDEQ(userID)).
		WithUser().
		WithTeam(func(query *dbent.TeamQuery) { query.WithOwner() }).
		Only(ctx)
	if dbent.IsNotFound(err) {
		return &TeamContext{Role: TeamRoleIndividual}, nil
	}
	if err != nil {
		return nil, err
	}
	teamEntity := membership.Edges.Team
	summary := teamSummaryFromEntity(teamEntity)
	member := teamMemberView(membership, membership.Edges.User)
	role := TeamRoleMember
	if membership.Status == TeamMembershipInvited {
		role = TeamMembershipInvited
	}
	return &TeamContext{
		Role:                role,
		MembershipStatus:    membership.Status,
		FinancialRestricted: membership.Status == TeamMembershipActive || membership.Status == TeamMembershipExitPending,
		Team:                &summary,
		CurrentMembership:   &member,
	}, nil
}

func (s *TeamService) ListMembers(ctx context.Context, ownerID int64) ([]TeamMemberView, error) {
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, false)
	if err != nil {
		return nil, err
	}
	memberships, err := s.client.TeamMembership.Query().
		Where(dbmembership.TeamIDEQ(teamEntity.ID)).
		WithUser().
		Order(dbent.Asc(dbmembership.FieldStatus), dbent.Asc(dbmembership.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	items := make([]TeamMemberView, 0, len(memberships))
	for _, membership := range memberships {
		items = append(items, teamMemberView(membership, membership.Edges.User))
	}
	return items, nil
}

func (s *TeamService) ListTransactions(ctx context.Context, userID int64, page, pageSize int) ([]TeamTransactionView, *pagination.PaginationResult, error) {
	teamEntity, err := s.client.Team.Query().Where(dbteam.OwnerIDEQ(userID)).Only(ctx)
	memberOnly := false
	if dbent.IsNotFound(err) {
		membership, membershipErr := s.client.TeamMembership.Query().Where(dbmembership.UserIDEQ(userID)).Only(ctx)
		if membershipErr != nil {
			return nil, nil, translateTeamMemberError(membershipErr)
		}
		teamEntity, err = s.client.Team.Get(ctx, membership.TeamID)
		memberOnly = true
	}
	if err != nil {
		return nil, nil, translateTeamError(err)
	}
	return s.listTransactions(ctx, teamEntity.ID, userID, memberOnly, page, pageSize)
}

func (s *TeamService) ListAdminTransactions(ctx context.Context, teamID int64, page, pageSize int) ([]TeamTransactionView, *pagination.PaginationResult, error) {
	if _, err := s.client.Team.Get(ctx, teamID); err != nil {
		return nil, nil, translateTeamError(err)
	}
	return s.listTransactions(ctx, teamID, 0, false, page, pageSize)
}

func (s *TeamService) listTransactions(ctx context.Context, teamID, memberID int64, memberOnly bool, page, pageSize int) ([]TeamTransactionView, *pagination.PaginationResult, error) {
	page, pageSize = normalizeTeamPagination(page, pageSize)
	query := s.client.TeamTransaction.Query().Where(dbtransaction.TeamIDEQ(teamID))
	if memberOnly {
		query = query.Where(dbtransaction.MemberIDEQ(memberID))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	entities, err := query.Order(dbent.Desc(dbtransaction.FieldCreatedAt)).Offset((page - 1) * pageSize).Limit(pageSize).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	memberIDs := make([]int64, 0, len(entities))
	seenMemberIDs := make(map[int64]struct{}, len(entities))
	for _, entity := range entities {
		if entity.MemberID == nil {
			continue
		}
		if _, exists := seenMemberIDs[*entity.MemberID]; exists {
			continue
		}
		seenMemberIDs[*entity.MemberID] = struct{}{}
		memberIDs = append(memberIDs, *entity.MemberID)
	}
	memberUsers := make(map[int64]*dbent.User, len(memberIDs))
	memberRemarks := make(map[int64]string, len(memberIDs))
	if len(memberIDs) > 0 {
		users, userErr := s.client.User.Query().Where(dbuser.IDIn(memberIDs...)).All(ctx)
		if userErr != nil {
			return nil, nil, userErr
		}
		for _, user := range users {
			memberUsers[user.ID] = user
		}
		memberships, membershipErr := s.client.TeamMembership.Query().Where(
			dbmembership.TeamIDEQ(teamID),
			dbmembership.UserIDIn(memberIDs...),
		).All(ctx)
		if membershipErr != nil {
			return nil, nil, membershipErr
		}
		for _, membership := range memberships {
			memberRemarks[membership.UserID] = membership.Remark
		}
	}
	items := make([]TeamTransactionView, 0, len(entities))
	for _, entity := range entities {
		var member *dbent.User
		var remark string
		if entity.MemberID != nil {
			member = memberUsers[*entity.MemberID]
			remark = memberRemarks[*entity.MemberID]
		}
		items = append(items, teamTransactionView(entity, member, remark))
	}
	return items, teamPagination(total, page, pageSize), nil
}

func (s *TeamService) ListAdminTeams(ctx context.Context, page, pageSize int, status, search string) ([]TeamSummary, *pagination.PaginationResult, error) {
	page, pageSize = normalizeTeamPagination(page, pageSize)
	query := s.client.Team.Query()
	if status == TeamStatusActive || status == TeamStatusSuspended {
		query = query.Where(dbteam.StatusEQ(status))
	}
	if search = strings.TrimSpace(search); search != "" {
		query = query.Where(dbteam.Or(
			dbteam.NameContainsFold(search),
			dbteam.HasOwnerWith(dbuser.Or(dbuser.EmailContainsFold(search), dbuser.UsernameContainsFold(search))),
		))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	entities, err := query.
		WithOwner().
		WithMemberships(func(query *dbent.TeamMembershipQuery) { query.WithUser() }).
		Order(dbent.Desc(dbteam.FieldCreatedAt)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	items := make([]TeamSummary, 0, len(entities))
	for _, entity := range entities {
		items = append(items, teamSummaryFromEntity(entity))
	}
	return items, teamPagination(total, page, pageSize), nil
}

func (s *TeamService) GetAdminOverview(ctx context.Context) (*TeamAdminOverview, error) {
	result := &TeamAdminOverview{}
	var err error
	if result.TotalTeams, err = s.client.Team.Query().Count(ctx); err != nil {
		return nil, fmt.Errorf("count teams: %w", err)
	}
	if result.ActiveTeams, err = s.client.Team.Query().Where(dbteam.StatusEQ(TeamStatusActive)).Count(ctx); err != nil {
		return nil, fmt.Errorf("count active teams: %w", err)
	}
	if result.SuspendedTeams, err = s.client.Team.Query().Where(dbteam.StatusEQ(TeamStatusSuspended)).Count(ctx); err != nil {
		return nil, fmt.Errorf("count suspended teams: %w", err)
	}
	activeMembers, err := s.client.TeamMembership.Query().Where(dbmembership.StatusEQ(TeamMembershipActive)).Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count active team members: %w", err)
	}
	if result.ExitPending, err = s.client.TeamMembership.Query().Where(dbmembership.StatusEQ(TeamMembershipExitPending)).Count(ctx); err != nil {
		return nil, fmt.Errorf("count pending team exits: %w", err)
	}
	result.MemberCount = activeMembers + result.ExitPending
	if result.PendingInvites, err = s.client.TeamMembership.Query().Where(dbmembership.StatusEQ(TeamMembershipInvited)).Count(ctx); err != nil {
		return nil, fmt.Errorf("count pending team invitations: %w", err)
	}
	var balances []struct {
		Total sql.NullFloat64 `json:"total"`
	}
	err = s.client.User.Query().Where(dbuser.Or(
		dbuser.HasOwnedTeam(),
		dbuser.HasTeamMembershipWith(dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending)),
	)).Aggregate(dbent.As(dbent.Sum(dbuser.FieldBalance), "total")).Scan(ctx, &balances)
	if err != nil {
		return nil, fmt.Errorf("sum team balances: %w", err)
	}
	if len(balances) > 0 && balances[0].Total.Valid {
		result.TotalBalance = balances[0].Total.Float64
	}
	return result, nil
}

func (s *TeamService) GetAdminTeam(ctx context.Context, teamID int64) (*TeamSummary, error) {
	entity, err := s.client.Team.Query().
		Where(dbteam.IDEQ(teamID)).
		WithOwner().
		WithMemberships(func(query *dbent.TeamMembershipQuery) { query.WithUser() }).
		Only(ctx)
	if err != nil {
		return nil, translateTeamError(err)
	}
	summary := teamSummaryFromEntity(entity)
	return &summary, nil
}

func (s *TeamService) ListUsage(ctx context.Context, ownerID int64, filter TeamUsageFilter) ([]TeamUsageItem, *pagination.PaginationResult, error) {
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, false)
	if err != nil {
		return nil, nil, err
	}
	return s.listTeamUsage(ctx, teamEntity.ID, filter)
}

func (s *TeamService) ListAdminUsage(ctx context.Context, teamID int64, filter TeamUsageFilter) ([]TeamUsageItem, *pagination.PaginationResult, error) {
	if _, err := s.client.Team.Get(ctx, teamID); err != nil {
		return nil, nil, translateTeamError(err)
	}
	return s.listTeamUsage(ctx, teamID, filter)
}

func (s *TeamService) listTeamUsage(ctx context.Context, teamID int64, filter TeamUsageFilter) ([]TeamUsageItem, *pagination.PaginationResult, error) {
	userIDs, err := s.teamUsageUserIDs(ctx, teamID)
	if err != nil {
		return nil, nil, err
	}
	if filter.MemberID > 0 && !containsTeamUser(userIDs, filter.MemberID) {
		return nil, nil, ErrTeamMemberNotFound
	}
	filter.Page, filter.PageSize = normalizeTeamPagination(filter.Page, filter.PageSize)
	query := s.client.UsageLog.Query().Where(dbusagelog.UserIDIn(userIDs...))
	if filter.MemberID > 0 {
		query = query.Where(dbusagelog.UserIDEQ(filter.MemberID))
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		query = query.Where(dbusagelog.ModelContainsFold(model))
	}
	if filter.Start != nil {
		query = query.Where(dbusagelog.CreatedAtGTE(*filter.Start))
	}
	if filter.End != nil {
		query = query.Where(dbusagelog.CreatedAtLT(*filter.End))
	}
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	logs, err := query.WithUser().Order(dbent.Desc(dbusagelog.FieldCreatedAt)).
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	items := make([]TeamUsageItem, 0, len(logs))
	for _, log := range logs {
		memberEmail := ""
		if log.Edges.User != nil {
			memberEmail = log.Edges.User.Email
		}
		items = append(items, TeamUsageItem{
			ID: log.ID, UserID: log.UserID, MemberEmail: memberEmail, RequestID: log.RequestID, Model: log.Model,
			InputTokens: log.InputTokens, OutputTokens: log.OutputTokens,
			CacheTokens: log.CacheCreationTokens + log.CacheReadTokens,
			TotalTokens: log.InputTokens + log.OutputTokens + log.CacheCreationTokens + log.CacheReadTokens,
			TotalCost:   log.TotalCost, ActualCost: log.ActualCost, DurationMS: log.DurationMs, CreatedAt: log.CreatedAt,
		})
	}
	return items, teamPagination(total, filter.Page, filter.PageSize), nil
}

func (s *TeamService) GetDashboard(ctx context.Context, ownerID int64, start, end time.Time) (*TeamDashboard, error) {
	teamEntity, err := s.requireOwnedTeam(ctx, ownerID, false)
	if err != nil {
		return nil, err
	}
	return s.getTeamDashboard(ctx, teamEntity.ID, start, end)
}

func (s *TeamService) GetAdminDashboard(ctx context.Context, teamID int64, start, end time.Time) (*TeamDashboard, error) {
	if _, err := s.client.Team.Get(ctx, teamID); err != nil {
		return nil, translateTeamError(err)
	}
	return s.getTeamDashboard(ctx, teamID, start, end)
}

func (s *TeamService) getTeamDashboard(ctx context.Context, teamID int64, start, end time.Time) (*TeamDashboard, error) {
	start, end = normalizeTeamRange(start, end)
	userIDs, err := s.teamUsageUserIDs(ctx, teamID)
	if err != nil {
		return nil, err
	}
	summary, err := s.GetAdminTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	result := &TeamDashboard{
		MemberCount: len(userIDs), TeamBalance: summary.TotalBalance, Start: start, End: end,
		Trend: []TeamTrendPoint{}, Members: []TeamMemberUsageStat{},
	}
	if s.sqlDB == nil || len(userIDs) == 0 {
		return result, nil
	}

	err = s.sqlDB.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0),
		       COALESCE(SUM(total_cost), 0),
		       COALESCE(SUM(actual_cost), 0)
		FROM usage_logs
		WHERE user_id = ANY($1) AND created_at >= $2 AND created_at < $3
	`, pq.Array(userIDs), start, end).Scan(&result.TotalRequests, &result.TotalTokens, &result.TotalCost, &result.ActualCost)
	if err != nil {
		return nil, fmt.Errorf("query team dashboard totals: %w", err)
	}

	trendRows, err := s.sqlDB.QueryContext(ctx, `
		SELECT TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
		       COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0),
		       COALESCE(SUM(actual_cost), 0)
		FROM usage_logs
		WHERE user_id = ANY($1) AND created_at >= $2 AND created_at < $3
		GROUP BY DATE_TRUNC('day', created_at AT TIME ZONE 'UTC')
		ORDER BY DATE_TRUNC('day', created_at AT TIME ZONE 'UTC')
	`, pq.Array(userIDs), start, end)
	if err != nil {
		return nil, fmt.Errorf("query team dashboard trend: %w", err)
	}
	defer func() { _ = trendRows.Close() }()
	for trendRows.Next() {
		var point TeamTrendPoint
		if err := trendRows.Scan(&point.Date, &point.Requests, &point.Tokens, &point.ActualCost); err != nil {
			return nil, err
		}
		result.Trend = append(result.Trend, point)
	}
	if err := trendRows.Err(); err != nil {
		return nil, err
	}

	memberRows, err := s.sqlDB.QueryContext(ctx, `
		SELECT u.id, u.email, u.username, COUNT(ul.id),
		       COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0),
		       COALESCE(SUM(ul.actual_cost), 0)
		FROM users u
		LEFT JOIN usage_logs ul ON ul.user_id = u.id AND ul.created_at >= $2 AND ul.created_at < $3
		WHERE u.id = ANY($1)
		GROUP BY u.id, u.email, u.username
		ORDER BY COALESCE(SUM(ul.actual_cost), 0) DESC, u.id
	`, pq.Array(userIDs), start, end)
	if err != nil {
		return nil, fmt.Errorf("query team member usage: %w", err)
	}
	defer func() { _ = memberRows.Close() }()
	for memberRows.Next() {
		var stat TeamMemberUsageStat
		if err := memberRows.Scan(&stat.UserID, &stat.Email, &stat.Username, &stat.Requests, &stat.Tokens, &stat.ActualCost); err != nil {
			return nil, err
		}
		result.Members = append(result.Members, stat)
	}
	return result, memberRows.Err()
}

func (s *TeamService) teamUsageUserIDs(ctx context.Context, teamID int64) ([]int64, error) {
	teamEntity, err := s.client.Team.Get(ctx, teamID)
	if err != nil {
		return nil, translateTeamError(err)
	}
	memberships, err := s.client.TeamMembership.Query().Where(
		dbmembership.TeamIDEQ(teamID),
		dbmembership.StatusIn(TeamMembershipActive, TeamMembershipExitPending),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(memberships)+1)
	userIDs = append(userIDs, teamEntity.OwnerID)
	for _, membership := range memberships {
		userIDs = append(userIDs, membership.UserID)
	}
	return userIDs, nil
}

// ResolveUsageUserIDs returns the usage scope visible to a regular user or team owner.
// Team members retain a personal scope; owners can optionally select one joined member.
func (s *TeamService) ResolveUsageUserIDs(ctx context.Context, userID, memberID int64) ([]int64, error) {
	teamContext, err := s.GetContext(ctx, userID)
	if err != nil {
		return nil, err
	}
	if teamContext.Role != TeamRoleOwner || teamContext.Team == nil {
		if memberID > 0 && memberID != userID {
			return nil, ErrTeamMemberNotFound
		}
		return []int64{userID}, nil
	}

	userIDs, err := s.teamUsageUserIDs(ctx, teamContext.Team.ID)
	if err != nil {
		return nil, err
	}
	if memberID == 0 {
		return userIDs, nil
	}
	if !containsTeamUser(userIDs, memberID) {
		return nil, ErrTeamMemberNotFound
	}
	return []int64{memberID}, nil
}

func teamSummaryFromEntity(entity *dbent.Team) TeamSummary {
	view := TeamSummary{ID: entity.ID, Name: entity.Name, Status: entity.Status, CreatedAt: entity.CreatedAt, Members: []TeamMemberView{}}
	if entity.Edges.Owner != nil {
		owner := entity.Edges.Owner
		view.Owner = TeamOwnerView{UserID: owner.ID, Email: owner.Email, Username: owner.Username, Balance: owner.Balance}
		view.TotalBalance = owner.Balance
	}
	for _, membership := range entity.Edges.Memberships {
		if membership.Edges.User == nil {
			continue
		}
		member := teamMemberView(membership, membership.Edges.User)
		view.Members = append(view.Members, member)
		switch membership.Status {
		case TeamMembershipInvited:
			view.PendingInviteCount++
		case TeamMembershipExitPending:
			view.ActiveMemberCount++
			view.ExitPendingCount++
			view.TotalBalance += member.Balance
		case TeamMembershipActive:
			view.ActiveMemberCount++
			view.TotalBalance += member.Balance
		}
	}
	return view
}

func teamTransactionView(entity *dbent.TeamTransaction, member *dbent.User, memberRemark string) TeamTransactionView {
	view := TeamTransactionView{
		ID: entity.ID, TeamID: entity.TeamID, OperatorID: entity.OperatorID, MemberID: entity.MemberID,
		Action: entity.Action, Amount: entity.Amount, OwnerBalanceBefore: entity.OwnerBalanceBefore,
		OwnerBalanceAfter: entity.OwnerBalanceAfter, MemberBalanceBefore: entity.MemberBalanceBefore,
		MemberBalanceAfter: entity.MemberBalanceAfter, Note: entity.Note, CreatedAt: entity.CreatedAt,
	}
	if member != nil {
		view.MemberEmail = member.Email
		view.MemberUsername = member.Username
		view.MemberRemark = memberRemark
	}
	return view
}

func normalizeTeamPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func teamPagination(total, page, pageSize int) *pagination.PaginationResult {
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	return &pagination.PaginationResult{Total: int64(total), Page: page, PageSize: pageSize, Pages: pages}
}

func normalizeTeamRange(start, end time.Time) (time.Time, time.Time) {
	now := time.Now().UTC()
	if end.IsZero() || end.After(now.Add(24*time.Hour)) {
		end = now
	}
	if start.IsZero() {
		start = end.AddDate(0, 0, -30)
	}
	if !start.Before(end) {
		start = end.AddDate(0, 0, -30)
	}
	if start.Before(end.AddDate(0, 0, -90)) {
		start = end.AddDate(0, 0, -90)
	}
	return start.UTC(), end.UTC()
}

func containsTeamUser(userIDs []int64, userID int64) bool {
	for _, candidate := range userIDs {
		if candidate == userID {
			return true
		}
	}
	return false
}
