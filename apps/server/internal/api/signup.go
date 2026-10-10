package api

import (
	"context"
	"net/http"

	"github.com/qunqin/openchat/apps/server/internal/httpx"
)

// Who may register, and what happens to them.
//
// Two switches in 后台 → 系统, both on unless an administrator turns them off,
// give three modes: closed (only administrators create accounts), open (a new
// account works at once) and approval (a new account is pending - it cannot
// sign in - until an administrator approves it). Two things sit above them:
//
//   - The first account ever created is the administrator, is never pending and
//     is never refused, so a fresh install cannot lock itself out.
//   - Accounts an administrator creates are never pending.
//
// A pending account has no session: Login refuses it before issuing one, and
// the session lookup (GetSessionUser) excludes it as a second lock.

const (
	// settingAllowRegistration is whether anyone may create an account by
	// registering. On unless an administrator turns it off.
	settingAllowRegistration = "allow_registration"
	// settingRegistrationApproval is whether such an account waits for an
	// administrator. On unless an administrator turns it off; it only matters
	// while registration is allowed.
	settingRegistrationApproval = "registration_approval"
)

var (
	errRegistrationClosed = httpx.NewError(http.StatusForbidden, "registration_closed",
		"registration is closed; ask an administrator for an account")
	errPendingApproval = httpx.NewError(http.StatusForbidden, "pending_approval",
		"this account is waiting for an administrator's approval")
)

// signupSwitches are the two switches as stored.
type signupSwitches struct {
	open, approval bool
}

// signupSwitches reads them. A setting that cannot be read is an error, not a
// default: either answer would be a guess about who is let in.
func (s *Server) signupSwitches(ctx context.Context) (signupSwitches, error) {
	open, err := boolSettingOr(ctx, s.q, settingAllowRegistration, true)
	if err != nil {
		return signupSwitches{}, err
	}
	approval, err := boolSettingOr(ctx, s.q, settingRegistrationApproval, true)
	if err != nil {
		return signupSwitches{}, err
	}
	return signupSwitches{open: open, approval: approval}, nil
}

// GetAuthOptions tells the sign-in pages what to offer. Registration shows as
// open while no account exists, because then it is: the first account is always
// let in, and it is not pending.
func (s *Server) GetAuthOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switches, err := s.signupSwitches(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	has, err := s.q.HasAccounts(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	registration := switches.open || !has
	httpx.JSON(w, http.StatusOK, AuthOptions{
		Registration: registration,
		Approval:     registration && has && switches.approval,
		Guest:        s.guestEnabled(ctx),
	})
}
