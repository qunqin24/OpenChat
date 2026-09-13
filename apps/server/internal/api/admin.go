package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

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

// CreateUser adds an account from the admin panel.
//
// Registration is the other way in, and differs in two ways that matter: the
// role is chosen here rather than derived from an empty table, and no session
// is issued — the admin is creating someone else's account, not signing into
// it.
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	var body CreateUserRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	email := strings.TrimSpace(string(body.Email))
	name := strings.TrimSpace(body.Name)

	switch {
	case email == "":
		httpx.Fail(w, r, httpx.NewFieldError("email", "email_required", "email is required"))
		return
	case name == "":
		httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "name is required"))
		return
	case len(body.Password) < minPasswordLength:
		httpx.Fail(w, r, httpx.NewFieldError("password", "password_short", "password is too short"))
		return
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	user, err := s.q.AdminCreateUser(r.Context(), sqlc.AdminCreateUserParams{
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         sqlc.UserRole(body.Role),
		Disabled:     body.Disabled != nil && *body.Disabled,
	})
	if err != nil {
		if dup := duplicateError(err); dup != nil {
			httpx.Fail(w, r, dup)
			return
		}
		httpx.Fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, toUser(user))
}

func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request, userId openapi_types.UUID) {
	var body UpdateUserRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Role == nil && body.Disabled == nil && body.Name == nil &&
		body.Email == nil && body.Password == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "nothing_to_update", "no changes were given"))
		return
	}

	var name, email, passwordHash *string

	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("name", "name_required", "name is required"))
			return
		}
		name = &trimmed
	}

	if body.Email != nil {
		trimmed := strings.TrimSpace(string(*body.Email))
		if trimmed == "" {
			httpx.Fail(w, r, httpx.NewFieldError("email", "email_required", "email is required"))
			return
		}
		email = &trimmed
	}

	if body.Password != nil {
		if len(*body.Password) < minPasswordLength {
			httpx.Fail(w, r, httpx.NewFieldError("password", "password_short", "password is too short"))
			return
		}
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		passwordHash = &hash
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

	// As in the bulk path, this cannot currently fire — the caller is an admin
	// and self-changes are refused above, so a second admin always exists — but
	// it keeps the invariant explicit rather than implied by two other rules.
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

	var role *sqlc.UserRole
	if body.Role != nil {
		converted := sqlc.UserRole(*body.Role)
		role = &converted
	}

	updated, err := s.q.AdminUpdateUser(r.Context(), sqlc.AdminUpdateUserParams{
		ID:           userId,
		Name:         name,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         role,
		Disabled:     body.Disabled,
	})
	if err != nil {
		if dup := duplicateError(err); dup != nil {
			httpx.Fail(w, r, dup)
			return
		}
		httpx.Fail(w, r, dbErr(err))
		return
	}

	// Two changes have to cut the person off now rather than whenever their
	// cookie expires: being disabled, and having their password replaced. The
	// session lookup already excludes disabled accounts, but a rotated
	// credential is only meaningful if the sessions it issued go with it.
	//
	// An admin who changes their own password is signed out by this too, which
	// is the point — the browser holding the old session cannot be assumed to
	// be theirs.
	if (body.Disabled != nil && *body.Disabled) || passwordHash != nil {
		if err := s.q.DeleteUserSessions(r.Context(), userId); err != nil {
			logWarn(r, "revoke sessions after account change", err)
		}
	}

	httpx.JSON(w, http.StatusOK, toUser(updated))
}

// maxBulkUsers mirrors the contract's cap on one request.
const maxBulkUsers = 200

// BulkUpdateUsers applies one change across several accounts in a single
// transaction.
//
// Accounts the caller cannot change are reported in `skipped` rather than
// failing the request: a batch is a selection, and one ineligible row in it is
// not a reason to reject the other forty-nine. A change that would leave the
// system with no enabled admin is refused outright, because that one is not
// recoverable through the UI.
func (s *Server) BulkUpdateUsers(w http.ResponseWriter, r *http.Request) {
	var body BulkUpdateUsersRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	switch {
	case len(body.UserIds) == 0:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "no_users", "no accounts were given"))
		return
	case len(body.UserIds) > maxBulkUsers:
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "too_many_users",
			fmt.Sprintf("at most %d accounts can be changed at once", maxBulkUsers)))
		return
	case (body.Role == nil) == (body.Disabled == nil):
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "one_change_required",
			"give exactly one of role or disabled"))
		return
	}

	actor := auth.MustUser(r.Context())

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	q := s.q.WithTx(tx)

	found, err := q.ListUsersByIDs(r.Context(), body.UserIds)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}

	present := make(map[uuid.UUID]sqlc.User, len(found))
	for _, user := range found {
		present[user.ID] = user
	}

	// Demoting or disabling yourself is the one change an admin cannot make;
	// promoting or re-enabling yourself is harmless, so it is not skipped.
	removesOwnAccess := (body.Role != nil && *body.Role != UserRole(sqlc.UserRoleAdmin)) ||
		(body.Disabled != nil && *body.Disabled)

	skipped := make([]BulkSkipped, 0)
	targets := make([]uuid.UUID, 0, len(body.UserIds))

	for _, id := range body.UserIds {
		switch {
		case present[id].ID == uuid.Nil:
			skipped = append(skipped, BulkSkipped{UserId: id, Reason: NotFound})
		case id == actor.ID && removesOwnAccess:
			skipped = append(skipped, BulkSkipped{UserId: id, Reason: Self})
		default:
			targets = append(targets, id)
		}
	}

	var updated int64
	if len(targets) > 0 {
		if body.Role != nil {
			updated, err = q.BulkUpdateUserRole(r.Context(), sqlc.BulkUpdateUserRoleParams{
				Ids:  targets,
				Role: sqlc.UserRole(*body.Role),
			})
		} else {
			updated, err = q.BulkSetUserDisabled(r.Context(), sqlc.BulkSetUserDisabledParams{
				Ids:      targets,
				Disabled: *body.Disabled,
			})
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}

		// A safety net rather than a reachable branch: the caller is an enabled
		// admin (RequireAdmin guarantees it) and is skipped from any change that
		// would remove their own access, so at least one admin always survives.
		// Kept because it asserts the invariant against the state the commit
		// would actually leave behind, and would catch a future change to the
		// skip rules that quietly broke it.
		admins, err := q.CountAdmins(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if admins == 0 {
			httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "last_admin",
				"this would leave no enabled admin"))
			return
		}

		if body.Disabled != nil && *body.Disabled {
			if err := q.DeleteSessionsForUsers(r.Context(), targets); err != nil {
				httpx.Fail(w, r, err)
				return
			}
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		httpx.Fail(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, BulkUpdateResult{Updated: int(updated), Skipped: skipped})
}
