package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Saving the moderation settings: all of them in one transaction, one save at a
// time, so that nothing reads, or is left with, a mixture.

// storedModeration is the stored moderation settings as the database holds them.
func (h *askHarness) storedModeration() map[string]string {
	h.t.Helper()
	rows, err := h.pool.Query(context.Background(), `SELECT key, value::text FROM app_settings WHERE key = ANY($1)`, moderationKeys)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			h.t.Fatal(err)
		}
		out[key] = value
	}
	return out
}

// A save that stops half way leaves the settings as they were: all of them are
// written in one transaction. The failure is a trigger refusing one value,
// raised after the keys before it (the mode and the on-error among them) were
// written - which, written one by one, stayed.
func TestInterruptedModerationSaveLeavesTheOldSettings(t *testing.T) {
	h, _ := newModerationHarness(t, `,"onError":"block","strikes":5`)
	ctx := context.Background()
	before := h.storedModeration()
	if len(before) != len(moderationKeys) {
		t.Fatalf("%d settings stored: %v", len(before), before)
	}

	for _, stmt := range []string{
		`CREATE FUNCTION mca_refuse_strikes() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				IF NEW.key = 'moderation_strikes' AND NEW.value = '7'::jsonb THEN RAISE EXCEPTION 'simulated failure'; END IF;
				RETURN NEW;
			END $$`,
		`CREATE TRIGGER mca_refuse_strikes BEFORE INSERT OR UPDATE ON app_settings FOR EACH ROW EXECUTE FUNCTION mca_refuse_strikes()`,
	} {
		if _, err := h.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS mca_refuse_strikes ON app_settings`)
		_, _ = h.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS mca_refuse_strikes()`)
	})

	rec := h.patchModeration(`{"mode":"off","onError":"allow","exemptAdmins":true,"strikes":7}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("the save should have failed: %d %s", rec.Code, rec.Body)
	}
	after := h.storedModeration()
	for _, key := range moderationKeys {
		if before[key] != after[key] {
			t.Errorf("%s: %s after a failed save, was %s", key, after[key], before[key])
		}
	}
	cfg, err := loadModeration(ctx, h.q)
	if err != nil || cfg.Mode != ModerationModeOpenai || cfg.OnError != ModerationOnErrorBlock || cfg.Strikes != 5 || cfg.ExemptAdmins {
		t.Errorf("settings now %+v (%v)", cfg, err)
	}
}

// Saves at once, each changing its own field, all stand: a save reads the
// settings under the lock, so it does not start from the ones before another.
func TestConcurrentModerationSavesDoNotUndoEachOther(t *testing.T) {
	h, _ := newModerationHarness(t, "")
	patches := []string{
		`{"strikes":11}`, `{"strikeWindowHours":12}`, `{"guestBanHours":13}`, `{"minScore":0.25}`,
		`{"exemptAdmins":true}`, `{"onError":"block"}`, `{"categories":["hate"]}`, `{"openaiBaseUrl":"http://localhost:1/v1"}`,
	}
	var wg sync.WaitGroup
	for _, patch := range patches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := h.patchModeration(patch); rec.Code != http.StatusOK {
				t.Errorf("%s: %d %s", patch, rec.Code, rec.Body)
			}
		}()
	}
	wg.Wait()
	cfg, err := loadModeration(context.Background(), h.q)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Strikes != 11 || cfg.StrikeWindowHours != 12 || cfg.GuestBanHours != 13 || cfg.MinScore != 0.25 || !cfg.ExemptAdmins ||
		cfg.OnError != ModerationOnErrorBlock || len(cfg.Categories) != 1 || cfg.Categories[0] != ModerationCategoryHate ||
		cfg.OpenAIBaseURL != "http://localhost:1/v1" {
		t.Errorf("a save was undone by another: %+v", cfg)
	}
}

// A message being checked while the settings are saved reads the settings from
// before the save or from after it, never a mixture of the two.
func TestModerationSettingsAreNeverReadHalfSaved(t *testing.T) {
	h, _ := newModerationHarness(t, "")
	one := `{"onError":"allow","strikes":2,"strikeWindowHours":5,"guestBanHours":7,"minScore":0.1,"exemptAdmins":false,"openaiBaseUrl":"http://localhost:1/a","categories":["hate"]}`
	two := `{"onError":"block","strikes":4,"strikeWindowHours":6,"guestBanHours":8,"minScore":0.2,"exemptAdmins":true,"openaiBaseUrl":"http://localhost:1/b","categories":["sexual","violence"]}`
	signature := func(c moderationConfig) string {
		var names []string
		for _, category := range c.Categories {
			names = append(names, string(category))
		}
		return strings.Join([]string{
			string(c.OnError), strings.Join(names, "+"), c.OpenAIBaseURL,
			strconv.Itoa(c.Strikes), strconv.Itoa(c.StrikeWindowHours), strconv.Itoa(c.GuestBanHours),
			strconv.FormatBool(c.ExemptAdmins), strconv.FormatFloat(c.MinScore, 'g', -1, 64),
		}, "|")
	}
	if rec := h.patchModeration(one); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	cfgOne, _ := loadModeration(context.Background(), h.q)
	if rec := h.patchModeration(two); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	cfgTwo, _ := loadModeration(context.Background(), h.q)
	valid := map[string]bool{signature(cfgOne): true, signature(cfgTwo): true}
	if len(valid) != 2 {
		t.Fatal("the two saves are not told apart")
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 2 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				cfg, err := loadModeration(context.Background(), h.q)
				if err != nil {
					t.Errorf("read: %v", err)
					return
				}
				if !valid[signature(cfg)] {
					t.Errorf("a mixture of two saves was read: %+v", cfg)
					return
				}
			}
		}()
	}
	for i := range 60 {
		patch := one
		if i%2 == 0 {
			patch = two
		}
		if rec := h.patchModeration(patch); rec.Code != http.StatusOK {
			t.Fatal(rec.Body)
		}
	}
	close(stop)
	readers.Wait()
}
