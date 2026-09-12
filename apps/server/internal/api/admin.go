package api

import (
	"net/http"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

const (
	defaultTrendDays = 30
	minTrendDays     = 7
	maxTrendDays     = 90
)

func (s *Server) GetAdminStats(w http.ResponseWriter, r *http.Request, params GetAdminStatsParams) {
	days := int32(defaultTrendDays)
	if params.Days != nil {
		days = int32(min(max(*params.Days, minTrendDays), maxTrendDays))
	}

	totals, err := s.q.GetDashboardTotals(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	rows, err := s.q.GetActivityTrend(r.Context(), days)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	trend := make([]TrendPoint, 0, len(rows))
	for _, row := range rows {
		trend = append(trend, TrendPoint{
			Day:           openapi_types.Date{Time: row.Day.Time},
			Messages:      row.Messages,
			Conversations: row.Conversations,
			Users:         row.Users,
		})
	}

	httpx.JSON(w, http.StatusOK, AdminStats{
		Totals: AdminTotals{
			TotalUsers:         totals.TotalUsers,
			ActiveUsers:        totals.ActiveUsers,
			TotalConversations: totals.TotalConversations,
			TotalMessages:      totals.TotalMessages,
			NewUsers7d:         totals.NewUsers7d,
			Messages7d:         totals.Messages7d,
		},
		Trend: trend,
	})
}

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request, params ListUsersParams) {
	limit := clampLimit(params.Limit)

	page := int32(1)
	if params.Page != nil && *params.Page > 1 {
		page = int32(*params.Page)
	}

	var search *string
	if params.Search != nil {
		if trimmed := strings.TrimSpace(*params.Search); trimmed != "" {
			search = &trimmed
		}
	}

	total, err := s.q.CountUsersFiltered(r.Context(), search)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	rows, err := s.q.ListUsers(r.Context(), sqlc.ListUsersParams{
		Search:    search,
		RowLimit:  limit,
		RowOffset: (page - 1) * limit,
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	items := make([]User, 0, len(rows))
	for _, row := range rows {
		items = append(items, toUser(row))
	}

	httpx.JSON(w, http.StatusOK, UserPage{
		Items: items,
		Total: total,
		Page:  int(page),
		Limit: int(limit),
	})
}

func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request, userId openapi_types.UUID) {
	var body UpdateUserRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Role == nil && body.Disabled == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update", "no changes were given"))
		return
	}

	actor := auth.MustUser(r.Context())
	target, err := s.q.GetUser(r.Context(), userId)
	if err != nil {
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// Two locks on the admin API: you cannot demote or disable yourself, and
	// the last enabled admin cannot be removed. Either would leave the panel
	// with nobody able to reach it and no way back in through the UI.
	demoting := body.Role != nil && *body.Role != UserRole(sqlc.UserRoleAdmin) && target.Role == sqlc.UserRoleAdmin
	disabling := body.Disabled != nil && *body.Disabled && !target.DisabledAt.Valid

	if target.ID == actor.ID && (demoting || disabling) {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "self_lockout",
			"you cannot remove your own admin access"))
		return
	}

	if demoting || (disabling && target.Role == sqlc.UserRoleAdmin) {
		admins, err := s.q.CountAdmins(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if admins <= 1 {
			httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "last_admin",
				"this is the last enabled admin"))
			return
		}
	}

	updated := target
	if body.Role != nil {
		updated, err = s.q.UpdateUserRole(r.Context(), sqlc.UpdateUserRoleParams{
			ID:   userId,
			Role: sqlc.UserRole(*body.Role),
		})
		if err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}
	}

	if body.Disabled != nil {
		updated, err = s.q.SetUserDisabled(r.Context(), sqlc.SetUserDisabledParams{
			ID:       userId,
			Disabled: *body.Disabled,
		})
		if err != nil {
			httpx.Fail(w, r, dbErr(err))
			return
		}

		// Disabling has to cut the person off now, not whenever their cookie
		// expires. The session lookup already excludes disabled accounts, but
		// dropping the rows also frees them and makes the effect visible.
		if *body.Disabled {
			if err := s.q.DeleteUserSessions(r.Context(), userId); err != nil {
				logWarn(r, "revoke sessions for disabled user", err)
			}
		}
	}

	httpx.JSON(w, http.StatusOK, toUser(updated))
}
