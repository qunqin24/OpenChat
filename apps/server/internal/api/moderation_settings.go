package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
)

// The content moderation settings (admin → system → moderation), each its own
// row of app_settings and read on every message, so a change applies at once.
const (
	// settingModerationMode is off (the default), openai or model.
	settingModerationMode = "moderation_mode"
	// settingModerationOpenAIKey is the OpenAI API key for openai mode. Write-
	// only through the API, like a channel's: reads say only whether one is
	// set and its last four characters. It is never logged.
	settingModerationOpenAIKey = "moderation_openai_key"
	// settingModerationOpenAIBaseURL overrides https://api.openai.com/v1.
	settingModerationOpenAIBaseURL = "moderation_openai_base_url"
	// settingModerationModel is the catalogue model that classifies in model mode.
	settingModerationModel = "moderation_model"
	// settingModerationCategories is the categories that block, a JSON array.
	settingModerationCategories = "moderation_categories"
	// settingModerationMinScore: above 0, a category whose score reaches it blocks too.
	settingModerationMinScore = "moderation_min_score"
	// settingModerationExemptAdmins lets an administrator's messages through
	// unchecked. Off by default: their messages reach the same upstream.
	settingModerationExemptAdmins = "moderation_exempt_admins"
	// settingModerationOnError is allow (the default) or block: what an
	// account's message does when the moderation service fails.
	settingModerationOnError = "moderation_on_error"
	// settingModerationStrikes is how many blocked messages within the window
	// sanction the account or address; 0 never does.
	settingModerationStrikes = "moderation_strikes"
	// settingModerationStrikeWindow is that window, in hours.
	settingModerationStrikeWindow = "moderation_strike_window_hours"

	defaultModerationStrikes     = 3
	defaultModerationWindowHours = 24
	maxModerationStrikes         = 1000
	maxModerationWindowHours     = 24 * 365
	defaultModerationOpenAIBase  = "https://api.openai.com/v1"
	moderationOpenAIModel        = "omni-moderation-latest"
	maxModerationBaseURLLength   = 500
)

// moderationCategories are OpenAI's categories, in the order they are listed.
var moderationCategories = []ModerationCategory{
	ModerationCategorySexual, ModerationCategorySexualMinors,
	ModerationCategoryHarassment, ModerationCategoryHarassmentThreatening,
	ModerationCategoryHate, ModerationCategoryHateThreatening,
	ModerationCategoryIllicit, ModerationCategoryIllicitViolent,
	ModerationCategorySelfHarm, ModerationCategorySelfHarmIntent, ModerationCategorySelfHarmInstructions,
	ModerationCategoryViolence, ModerationCategoryViolenceGraphic,
}

// defaultBlockedCategories is what blocks until an administrator chooses: the
// categories nobody should be sending a model. In the order of the list above.
var defaultBlockedCategories = []ModerationCategory{
	ModerationCategorySexualMinors, ModerationCategoryHarassmentThreatening, ModerationCategoryHateThreatening,
	ModerationCategoryIllicitViolent, ModerationCategorySelfHarmInstructions, ModerationCategoryViolenceGraphic,
}

// moderationConfig is the settings as the checks read them.
type moderationConfig struct {
	Mode              ModerationMode
	OpenAIKey         string
	OpenAIBaseURL     string
	Model             string
	Categories        []ModerationCategory
	MinScore          float64
	ExemptAdmins      bool
	OnError           ModerationOnError
	Strikes           int
	StrikeWindowHours int

	// modeRead is whether the stored mode was read for certain (an absent mode is
	// off, and read). What loadModeration returns beside an error is what it
	// could read, and this says whether the one setting that decides if
	// moderation applies at all can be trusted.
	modeRead bool
}

func defaultModerationConfig() moderationConfig {
	return moderationConfig{
		modeRead:          true,
		Mode:              ModerationModeOff,
		Categories:        slices.Clone(defaultBlockedCategories),
		OnError:           ModerationOnErrorAllow,
		Strikes:           defaultModerationStrikes,
		StrikeWindowHours: defaultModerationWindowHours,
	}
}

var moderationKeys = []string{
	settingModerationMode, settingModerationOpenAIKey, settingModerationOpenAIBaseURL, settingModerationModel,
	settingModerationCategories, settingModerationMinScore, settingModerationExemptAdmins, settingModerationOnError,
	settingModerationStrikes, settingModerationStrikeWindow,
}

// loadModeration reads the settings in one query. A value that does not parse is
// an error, not a default: a mode that silently read as "off" would be
// moderation switched off by a typo. Every row is read even so, so that the
// settings returned beside the error are as much as could be read (and
// modeRead says whether the mode was among it): the check refuses a message
// when the settings are not all readable, unless the mode that was read is off.
func loadModeration(ctx context.Context, q *sqlc.Queries) (moderationConfig, error) {
	c := defaultModerationConfig()
	rows, err := q.GetSettings(ctx, moderationKeys)
	if err != nil {
		c.modeRead = false
		return c, err
	}
	var failed []error
	for _, row := range rows {
		var err error
		switch row.Key {
		case settingModerationMode:
			var v string
			if err = json.Unmarshal(row.Value, &v); err == nil {
				c.Mode = ModerationMode(v)
				if !c.Mode.Valid() {
					err = fmt.Errorf("unknown mode %q", v)
				}
			}
		case settingModerationOpenAIKey:
			err = json.Unmarshal(row.Value, &c.OpenAIKey)
		case settingModerationOpenAIBaseURL:
			err = json.Unmarshal(row.Value, &c.OpenAIBaseURL)
		case settingModerationModel:
			err = json.Unmarshal(row.Value, &c.Model)
		case settingModerationCategories:
			c.Categories = nil
			err = json.Unmarshal(row.Value, &c.Categories)
		case settingModerationMinScore:
			err = json.Unmarshal(row.Value, &c.MinScore)
		case settingModerationExemptAdmins:
			err = json.Unmarshal(row.Value, &c.ExemptAdmins)
		case settingModerationOnError:
			var v string
			if err = json.Unmarshal(row.Value, &v); err == nil {
				c.OnError = ModerationOnError(v)
				if !c.OnError.Valid() {
					err = fmt.Errorf("unknown value %q", v)
				}
			}
		case settingModerationStrikes:
			err = json.Unmarshal(row.Value, &c.Strikes)
		case settingModerationStrikeWindow:
			err = json.Unmarshal(row.Value, &c.StrikeWindowHours)
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("moderation setting %s: %w", row.Key, err))
			if row.Key == settingModerationMode {
				c.modeRead = false
			}
		}
	}
	return c, errors.Join(failed...)
}

func (c moderationConfig) toSettings() ModerationSettings {
	out := ModerationSettings{
		Mode:              c.Mode,
		OpenaiKeySet:      c.OpenAIKey != "",
		OpenaiBaseUrl:     c.OpenAIBaseURL,
		Model:             c.Model,
		Categories:        slices.Clone(c.Categories),
		MinScore:          c.MinScore,
		ExemptAdmins:      c.ExemptAdmins,
		OnError:           c.OnError,
		Strikes:           c.Strikes,
		StrikeWindowHours: c.StrikeWindowHours,
	}
	if out.Categories == nil {
		out.Categories = []ModerationCategory{}
	}
	if preview := keyPreview(c.OpenAIKey); preview != "" {
		out.OpenaiKeyPreview = &preview
	}
	return out
}

func (s *Server) GetModerationSettings(w http.ResponseWriter, r *http.Request) {
	c, err := loadModeration(r.Context(), s.q)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, c.toSettings())
}

func invalidModeration(code, message string) error {
	return httpx.NewError(http.StatusBadRequest, code, message)
}

// moderationSaving serializes the saves within a process; the advisory lock
// (LockModerationSettings) does it between processes.
var moderationSaving sync.Mutex

// UpdateModerationSettings validates the whole change against the settings it
// would leave behind, and only then saves any of it - all of it in one
// transaction. Saved key by key, a save that stopped half way left a mixture
// (a model mode with no model, an on-error of allow beside a stored block), and
// a message checked in the middle of one read the mixture. The settings are
// read, changed and written under one lock, so two saves at once apply one after
// the other instead of each undoing the other's fields, and the readers - one
// query each - see the settings from before the save or from after it.
func (s *Server) UpdateModerationSettings(w http.ResponseWriter, r *http.Request) {
	var body UpdateModerationSettingsRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	ctx := r.Context()

	// One save at a time in this process too: each save holds a connection while
	// it waits for the lock, and the check of a model needs another, so saves
	// as many as the pool has connections would wait for each other for good.
	moderationSaving.Lock()
	defer moderationSaving.Unlock()

	var c moderationConfig
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockModerationSettings(ctx); err != nil {
			return err
		}
		var err error
		if c, err = loadModeration(ctx, q); err != nil {
			// A stored value that no longer parses must not stop the operator from
			// replacing it: start from the defaults over what does.
			slog.WarnContext(ctx, "moderation settings unreadable; the change starts from the defaults", "error", err)
			c = defaultModerationConfig()
		}
		if err := c.apply(body); err != nil {
			return err
		}
		if err := s.checkModerationMode(ctx, c); err != nil {
			return err
		}
		return saveModeration(ctx, q, c)
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Never the key itself, not even at debug level.
	slog.InfoContext(ctx, "moderation settings changed",
		"mode", c.Mode, "model", c.Model, "categories", len(c.Categories), "on_error", c.OnError,
		"strikes", c.Strikes, "key_changed", body.OpenaiApiKey != nil, "actor", auth.MustUser(ctx).Email)

	httpx.JSON(w, http.StatusOK, c.toSettings())
}

// apply lays a change over the settings, refusing a field that is not valid. The
// settings are only changed in memory: nothing is saved here.
func (c *moderationConfig) apply(body UpdateModerationSettingsRequest) error {
	if body.Mode != nil {
		if !body.Mode.Valid() {
			return invalidModeration("invalid_moderation_mode", "the mode is off, openai or model")
		}
		c.Mode = *body.Mode
	}
	if body.OpenaiApiKey != nil {
		c.OpenAIKey = strings.TrimSpace(*body.OpenaiApiKey)
	}
	if body.OpenaiBaseUrl != nil {
		base, err := cleanModerationBaseURL(*body.OpenaiBaseUrl)
		if err != nil {
			return err
		}
		c.OpenAIBaseURL = base
	}
	if body.Model != nil {
		c.Model = strings.TrimSpace(*body.Model)
	}
	if body.Categories != nil {
		seen := map[ModerationCategory]bool{}
		var chosen []ModerationCategory
		for _, category := range *body.Categories {
			if !category.Valid() {
				return invalidModeration("invalid_moderation_category", "unknown category "+string(category))
			}
			if !seen[category] {
				seen[category] = true
				chosen = append(chosen, category)
			}
		}
		// In the order the categories are listed, whichever order they came in.
		slices.SortStableFunc(chosen, func(a, b ModerationCategory) int {
			return slices.Index(moderationCategories, a) - slices.Index(moderationCategories, b)
		})
		c.Categories = chosen
	}
	if body.MinScore != nil {
		if math.IsNaN(*body.MinScore) || *body.MinScore < 0 || *body.MinScore > 1 {
			return invalidModeration("invalid_moderation_score", "the minimum score must be between 0 and 1")
		}
		c.MinScore = *body.MinScore
	}
	if body.ExemptAdmins != nil {
		c.ExemptAdmins = *body.ExemptAdmins
	}
	if body.OnError != nil {
		if !body.OnError.Valid() {
			return invalidModeration("invalid_moderation_on_error", "on error is allow or block")
		}
		c.OnError = *body.OnError
	}
	if body.Strikes != nil {
		if *body.Strikes < 0 || *body.Strikes > maxModerationStrikes {
			return invalidModeration("invalid_moderation_strikes",
				fmt.Sprintf("strikes must be between 0 and %d", maxModerationStrikes))
		}
		c.Strikes = *body.Strikes
	}
	if body.StrikeWindowHours != nil {
		if *body.StrikeWindowHours < 1 || *body.StrikeWindowHours > maxModerationWindowHours {
			return invalidModeration("invalid_moderation_window",
				fmt.Sprintf("the strike window must be between 1 and %d hours", maxModerationWindowHours))
		}
		c.StrikeWindowHours = *body.StrikeWindowHours
	}
	return nil
}

// checkModerationMode is what the chosen mode needs, as the settings would
// stand after the change.
func (s *Server) checkModerationMode(ctx context.Context, c moderationConfig) error {
	switch c.Mode {
	case ModerationModeOpenai:
		if c.OpenAIKey == "" {
			return httpx.NewFieldError("openaiApiKey", "moderation_key_required", "an OpenAI API key is needed for this mode")
		}
	case ModerationModeModel:
		if c.Model == "" {
			return httpx.NewFieldError("model", "moderation_model_required", "choose the model that classifies messages")
		}
		// Whether it can be answered with, not whether it has money left: at its
		// cap the check is unavailable (and the setting says what that means).
		if _, _, err := s.pickChannel(ctx, c.Model, asker{staff: true, admin: true, ignoreCap: true}); err != nil {
			return err
		}
	}
	return nil
}

// saveModeration writes every setting. The caller's transaction is what makes
// them one change.
func saveModeration(ctx context.Context, q *sqlc.Queries, c moderationConfig) error {
	for _, set := range []struct {
		key   string
		value any
	}{
		{settingModerationMode, string(c.Mode)},
		{settingModerationOpenAIKey, c.OpenAIKey},
		{settingModerationOpenAIBaseURL, c.OpenAIBaseURL},
		{settingModerationModel, c.Model},
		{settingModerationCategories, categoriesOrEmpty(c.Categories)},
		{settingModerationMinScore, c.MinScore},
		{settingModerationExemptAdmins, c.ExemptAdmins},
		{settingModerationOnError, string(c.OnError)},
		{settingModerationStrikes, c.Strikes},
		{settingModerationStrikeWindow, c.StrikeWindowHours},
	} {
		value, err := json.Marshal(set.value)
		if err != nil {
			return err
		}
		if err := q.SetSetting(ctx, sqlc.SetSettingParams{Key: set.key, Value: value}); err != nil {
			return err
		}
	}
	return nil
}

func categoriesOrEmpty(c []ModerationCategory) []ModerationCategory {
	if c == nil {
		return []ModerationCategory{}
	}
	return c
}

// cleanModerationBaseURL is a base URL as stored: empty (OpenAI's own), or an
// http(s) address without a trailing slash.
func cleanModerationBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || len(raw) > maxModerationBaseURLLength {
		return "", httpx.NewFieldError("openaiBaseUrl", "invalid_moderation_base_url", "the base URL must be an http(s) address")
	}
	return raw, nil
}
