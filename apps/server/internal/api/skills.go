package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/skills"
)

// Skills (package skills, docs/design/skills-plan.md): a person's own under
// /me/skills, the site's under /admin/skills. The two sets of handlers are one
// set over an owner - the caller's id from the session, or nil for the site -
// and every query is scoped to it, so nobody reaches another person's skill
// and the admin endpoints reach no personal one.
//
// A person's skill is moderated as it is saved, like their custom
// instructions (moderation_text.go); the site's is the administrator's and is
// not. What goes into a conversation is decided when it takes its snapshot
// (memory.Snapshot), which also checks a version saved while moderation was off.

// skillBodyMax bounds a skill's JSON body: its text is at most
// skills.MaxTextChars characters, up to four bytes each, escaped.
const skillBodyMax = 4 << 20

func (s *Server) skills() *skills.Store { return skills.NewStore(s.pool) }

// skillOwner is whose skills a request is about: the caller's for /me, nil
// (the site's) for /admin, or a plugin's (plugin set, id nil) for
// /admin/plugins/{id}/skills - administrators' writing, as the site's.
type skillOwner struct {
	id     *uuid.UUID
	plugin *uuid.UUID
	user   sqlc.User
}

func (o skillOwner) site() bool { return o.id == nil }

// skillStore is the store of the owner's skills.
func (s *Server) skillStore(o skillOwner) *skills.Store {
	if o.plugin != nil {
		return s.skills().ForPlugin(*o.plugin)
	}
	return s.skills()
}

func personalOwner(r *http.Request) skillOwner {
	user := auth.MustUser(r.Context())
	return skillOwner{id: &user.ID, user: user}
}

func siteOwner(r *http.Request) skillOwner {
	return skillOwner{user: auth.MustUser(r.Context())}
}

func (s *Server) ListMySkills(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	owner := personalOwner(r)
	own, err := s.skills().List(ctx, owner.id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	site, err := s.skills().List(ctx, nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	enabled, allowed, err := s.skillSwitches(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	builtin, err := s.builtinSkills(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := MySkillList{Items: skillViews(own), Site: []Skill{}, Builtin: []BuiltinSkill{}, Allowed: allowed, Enabled: enabled, Limit: skills.MaxPersonal}
	for _, skill := range site {
		if skill.Enabled {
			out.Site = append(out.Site, skillView(skill))
		}
	}
	for _, b := range builtin {
		if b.Enabled {
			out.Builtin = append(out.Builtin, b)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) ListSiteSkills(w http.ResponseWriter, r *http.Request) {
	rows, err := s.skills().List(r.Context(), nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	builtin, err := s.builtinSkills(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, SkillList{Items: skillViews(rows), Builtin: builtin, Limit: skills.MaxSite})
}

func (s *Server) CreateMySkill(w http.ResponseWriter, r *http.Request) {
	s.saveSkill(w, r, personalOwner(r), nil)
}

func (s *Server) CreateSiteSkill(w http.ResponseWriter, r *http.Request) {
	s.saveSkill(w, r, siteOwner(r), nil)
}

func (s *Server) UpdateMySkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.saveSkill(w, r, personalOwner(r), &skillID)
}

func (s *Server) UpdateSiteSkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.saveSkill(w, r, siteOwner(r), &skillID)
}

func (s *Server) GetMySkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.getSkill(w, r, personalOwner(r), skillID)
}

func (s *Server) GetSiteSkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.getSkill(w, r, siteOwner(r), skillID)
}

func (s *Server) SetMySkillEnabled(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.setSkillEnabled(w, r, personalOwner(r), skillID)
}

func (s *Server) SetSiteSkillEnabled(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.setSkillEnabled(w, r, siteOwner(r), skillID)
}

func (s *Server) DeleteMySkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.deleteSkill(w, r, personalOwner(r), skillID)
}

func (s *Server) DeleteSiteSkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.deleteSkill(w, r, siteOwner(r), skillID)
}

func (s *Server) ImportMySkills(w http.ResponseWriter, r *http.Request, params ImportMySkillsParams) {
	s.importSkills(w, r, personalOwner(r), params.Filename)
}

func (s *Server) ImportSiteSkills(w http.ResponseWriter, r *http.Request, params ImportSiteSkillsParams) {
	s.importSkills(w, r, siteOwner(r), params.Filename)
}

// skillSwitches are the administrator's two switches: skills at all, and
// personal ones.
func (s *Server) skillSwitches(r *http.Request) (enabled, personal bool, err error) {
	if enabled, err = boolSettingOr(r.Context(), s.q, skills.SettingEnabled, true); err != nil {
		return false, false, err
	}
	personal, err = boolSettingOr(r.Context(), s.q, skills.SettingPersonal, true)
	return enabled, personal, err
}

var errPersonalSkillsOff = httpx.NewError(http.StatusForbidden, "personal_skills_off",
	"personal skills are turned off on this server")

// mayWrite refuses writing a personal skill while the administrator has
// personal skills off. Switching one off and deleting it are still allowed.
func (s *Server) mayWrite(r *http.Request, owner skillOwner) error {
	if owner.site() {
		return nil
	}
	_, personal, err := s.skillSwitches(r)
	if err != nil {
		return err
	}
	if !personal {
		return errPersonalSkillsOff
	}
	return nil
}

func (s *Server) saveSkill(w http.ResponseWriter, r *http.Request, owner skillOwner, id *uuid.UUID) {
	if err := s.mayWrite(r, owner); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var body SkillRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, skillBodyMax))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_body", "request body is not valid JSON: "+err.Error()))
		return
	}
	files := make([]skills.File, len(body.Files))
	for i, f := range body.Files {
		files[i] = skills.File{Path: f.Path, Content: f.Content}
	}
	skill, err := s.storeSkill(r, owner, id, files)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if id == nil {
		status = http.StatusCreated
	}
	s.respondSkill(w, r, owner, skill.ID, status)
}

// storeSkill checks a skill's files and saves them: moderated first when they
// are a person's, the way their custom instructions are, so a refusal saves
// nothing.
func (s *Server) storeSkill(r *http.Request, owner skillOwner, id *uuid.UUID, files []skills.File) (skills.Skill, error) {
	ctx := r.Context()
	parsed, err := skills.Parse(files)
	if err != nil {
		return skills.Skill{}, skillError(err)
	}
	var checked *string
	if owner.site() {
		checked = &parsed.Hash
	} else {
		ok, err := s.moderateSaved(ctx, owner.user, ModerationSourceSkill, parsed.Text)
		if err != nil {
			return skills.Skill{}, err
		}
		if ok {
			checked = &parsed.Hash
		}
	}
	skill, err := s.skillStore(owner).Save(ctx, owner.id, id, parsed, checked, &owner.user.ID)
	if err != nil {
		return skills.Skill{}, skillError(err)
	}
	switch {
	case owner.plugin != nil:
		slog.InfoContext(ctx, "plugin skill saved", "skill", skill.Name, "plugin_id", *owner.plugin, "actor", owner.user.Email)
	case owner.site():
		slog.InfoContext(ctx, "site skill saved", "skill", skill.Name, "actor", owner.user.Email)
	}
	return skill, nil
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID) {
	s.respondSkill(w, r, owner, id, http.StatusOK)
}

func (s *Server) respondSkill(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID, status int) {
	skill, files, err := s.skillStore(owner).Get(r.Context(), owner.id, id)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	out := SkillDetail{Skill: skillView(skill), Files: make([]SkillFile, len(files))}
	for i, f := range files {
		out.Files[i] = SkillFile{Path: f.Path, Content: f.Content}
	}
	httpx.JSON(w, status, out)
}

func (s *Server) setSkillEnabled(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID) {
	var body SkillEnabledRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// Switching one back on is writing it into new conversations again.
	if body.Enabled {
		if err := s.mayWrite(r, owner); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	if err := s.skillStore(owner).SetEnabled(r.Context(), owner.id, id, body.Enabled); err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID) {
	if err := s.skillStore(owner).Delete(r.Context(), owner.id, id); err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	if owner.site() {
		slog.InfoContext(r.Context(), "site skill deleted", "skill_id", id, "actor", owner.user.Email)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) importSkills(w http.ResponseWriter, r *http.Request, owner skillOwner, filename string) {
	if err := s.mayWrite(r, owner); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, skills.MaxArchiveBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "the file is larger than 8 MB"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	unpacked, err := skills.Unpack(filename, data)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	out := SkillImportResult{
		Created: []Skill{},
		Failed: []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Name    string `json:"name"`
		}{},
		Skipped: unpacked.Skipped,
	}
	if out.Skipped == nil {
		out.Skipped = []string{}
	}
	for _, files := range unpacked.Skills {
		skill, err := s.storeSkill(r, owner, nil, files)
		if err == nil {
			out.Created = append(out.Created, skillView(skill))
			continue
		}
		var apiErr *httpx.APIError
		if !errors.As(err, &apiErr) {
			httpx.Fail(w, r, err)
			return
		}
		out.Failed = append(out.Failed, struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Name    string `json:"name"`
		}{Code: apiErr.Code, Message: apiErr.Message, Name: importedName(files)})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// importedName is how a skill that could not be saved is named in the result:
// its frontmatter's name if it gives one, else the folder of its files.
func importedName(files []skills.File) string {
	for _, f := range files {
		if f.Path == skills.Entry {
			if meta, _, err := skills.Frontmatter(f.Content); err == nil {
				return meta.Name
			}
		}
	}
	if len(files) > 0 {
		return path.Dir(files[0].Path)
	}
	return ""
}

// skillError is a store's error as the API answers it.
func skillError(err error) error {
	var bad *skills.InvalidError
	switch {
	case errors.As(err, &bad):
		return httpx.NewFieldError("files", bad.Code, bad.Message)
	case errors.Is(err, skills.ErrNotFound):
		return httpx.NewError(http.StatusNotFound, "not_found", "no such skill")
	case errors.Is(err, skills.ErrNameTaken):
		return httpx.NewError(http.StatusConflict, "skill_name_taken", err.Error())
	case errors.Is(err, skills.ErrFull):
		return httpx.NewError(http.StatusConflict, "skill_limit", err.Error())
	}
	return err
}

func skillViews(rows []skills.Skill) []Skill {
	out := make([]Skill, len(rows))
	for i, row := range rows {
		out[i] = skillView(row)
	}
	return out
}

func skillView(skill skills.Skill) Skill {
	source := SkillSourcePersonal
	if skill.OwnerID == nil {
		source = SkillSourceSite
	}
	return Skill{
		Id: skill.ID, Name: skill.Name, Description: skill.Description, Enabled: skill.Enabled,
		Source: source, Checked: skill.Checked, Blocked: skill.Blocked, UpdatedAt: skill.UpdatedAt,
	}
}

// ------------------------------------------------------------ built-in skills

// builtinOff is the built-in skills an administrator switched off.
func (s *Server) builtinOff(r *http.Request) (map[string]bool, error) {
	raw, err := s.q.GetSetting(r.Context(), skills.SettingBuiltinOff)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, err
	}
	off := map[string]bool{}
	for _, name := range names {
		off[name] = true
	}
	return off, nil
}

func (s *Server) builtinSkills(r *http.Request) ([]BuiltinSkill, error) {
	off, err := s.builtinOff(r)
	if err != nil {
		return nil, err
	}
	out := []BuiltinSkill{}
	for _, b := range skills.Builtins() {
		out = append(out, BuiltinSkill{Name: b.Name, Description: b.Description, Enabled: !off[b.Name]})
	}
	return out, nil
}

func (s *Server) GetBuiltinSkill(w http.ResponseWriter, r *http.Request, name string) {
	b, ok := skills.Builtin(name)
	if !ok {
		httpx.Fail(w, r, skillError(skills.ErrNotFound))
		return
	}
	off, err := s.builtinOff(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, BuiltinSkillDetail{
		Skill: BuiltinSkill{Name: b.Name, Description: b.Description, Enabled: !off[b.Name]},
		Files: fileViews(b.Files),
	})
}

func (s *Server) SetBuiltinSkillEnabled(w http.ResponseWriter, r *http.Request, name string) {
	var body SkillEnabledRequest
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if _, ok := skills.Builtin(name); !ok {
		httpx.Fail(w, r, skillError(skills.ErrNotFound))
		return
	}
	off, err := s.builtinOff(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if body.Enabled {
		delete(off, name)
	} else {
		off[name] = true
	}
	names := make([]string, 0, len(off))
	for n := range off {
		names = append(names, n)
	}
	sort.Strings(names)
	value, _ := json.Marshal(names)
	if err := s.q.SetSetting(r.Context(), sqlc.SetSettingParams{Key: skills.SettingBuiltinOff, Value: value}); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	slog.InfoContext(r.Context(), "built-in skill switched", "skill", name, "enabled", body.Enabled, "actor", auth.MustUser(r.Context()).Email)
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------- versions and export

func (s *Server) ListMySkillVersions(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.listSkillVersions(w, r, personalOwner(r), skillID)
}

func (s *Server) ListSiteSkillVersions(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.listSkillVersions(w, r, siteOwner(r), skillID)
}

func (s *Server) GetMySkillVersion(w http.ResponseWriter, r *http.Request, skillID, versionID openapi_types.UUID) {
	s.getSkillVersion(w, r, personalOwner(r), skillID, versionID)
}

func (s *Server) GetSiteSkillVersion(w http.ResponseWriter, r *http.Request, skillID, versionID openapi_types.UUID) {
	s.getSkillVersion(w, r, siteOwner(r), skillID, versionID)
}

func (s *Server) ExportMySkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.exportSkill(w, r, personalOwner(r), skillID)
}

func (s *Server) ExportSiteSkill(w http.ResponseWriter, r *http.Request, skillID openapi_types.UUID) {
	s.exportSkill(w, r, siteOwner(r), skillID)
}

func (s *Server) listSkillVersions(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID) {
	rows, err := s.skills().Versions(r.Context(), owner.id, id)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	out := SkillVersionList{Items: make([]SkillVersion, len(rows))}
	for i, v := range rows {
		out.Items[i] = SkillVersion{Id: v.ID, Description: v.Description, CreatedAt: v.CreatedAt, Current: v.Current}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) getSkillVersion(w http.ResponseWriter, r *http.Request, owner skillOwner, id, versionID uuid.UUID) {
	versions, err := s.skills().Versions(r.Context(), owner.id, id)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	files, err := s.skills().OwnedVersion(r.Context(), owner.id, id, versionID)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	out := SkillVersionDetail{Files: fileViews(files)}
	for _, v := range versions {
		if v.ID == versionID {
			out.Version = SkillVersion{Id: v.ID, Description: v.Description, CreatedAt: v.CreatedAt, Current: v.Current}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Server) exportSkill(w http.ResponseWriter, r *http.Request, owner skillOwner, id uuid.UUID) {
	skill, files, err := s.skillStore(owner).Get(r.Context(), owner.id, id)
	if err != nil {
		httpx.Fail(w, r, skillError(err))
		return
	}
	data, err := skills.Zip(skill.Name, files)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+skill.Name+`.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ----------------------------------------------------------------- picker

// ListAvailableSkills is what the composer's skill picker offers: what the
// conversation's model can open (its snapshot), or what a new one would list.
func (s *Server) ListAvailableSkills(w http.ResponseWriter, r *http.Request, params ListAvailableSkillsParams) {
	user := auth.MustUser(r.Context())
	list, err := s.notes().Skills(r.Context(), user.ID, params.ConversationId)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "not_found", "no such conversation"))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := AvailableSkillList{Items: make([]AvailableSkill, 0, len(list))}
	for _, skill := range list {
		source := AvailableSkillSourceSite
		switch {
		case skill.Builtin:
			source = AvailableSkillSourceBuiltin
		case skill.Personal:
			source = AvailableSkillSourcePersonal
		}
		out.Items = append(out.Items, AvailableSkill{Name: skill.Name, Description: skill.Description, Source: source})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func fileViews(files []skills.File) []SkillFile {
	out := make([]SkillFile, len(files))
	for i, f := range files {
		out[i] = SkillFile{Path: f.Path, Content: f.Content}
	}
	return out
}

// ------------------------------------------------------- a plugin's skills

// pluginOwner is the owner of a plugin's skills, once the plugin is found.
func (s *Server) pluginOwner(w http.ResponseWriter, r *http.Request, pluginID uuid.UUID) (skillOwner, bool) {
	if _, err := s.q.GetPlugin(r.Context(), pluginID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errPluginNotFound
		}
		httpx.Fail(w, r, err)
		return skillOwner{}, false
	}
	return skillOwner{plugin: &pluginID, user: auth.MustUser(r.Context())}, true
}

func (s *Server) ListPluginSkills(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	owner, ok := s.pluginOwner(w, r, pluginId)
	if !ok {
		return
	}
	rows, err := s.skillStore(owner).List(r.Context(), nil)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// No built-in ones: those are the site's.
	httpx.JSON(w, http.StatusOK, SkillList{Items: skillViews(rows), Builtin: []BuiltinSkill{}, Limit: skills.MaxSite})
}

func (s *Server) CreatePluginSkill(w http.ResponseWriter, r *http.Request, pluginId openapi_types.UUID) {
	if owner, ok := s.pluginOwner(w, r, pluginId); ok {
		s.saveSkill(w, r, owner, nil)
	}
}

func (s *Server) GetPluginSkill(w http.ResponseWriter, r *http.Request, pluginId, skillID openapi_types.UUID) {
	if owner, ok := s.pluginOwner(w, r, pluginId); ok {
		s.getSkill(w, r, owner, skillID)
	}
}

func (s *Server) UpdatePluginSkill(w http.ResponseWriter, r *http.Request, pluginId, skillID openapi_types.UUID) {
	if owner, ok := s.pluginOwner(w, r, pluginId); ok {
		s.saveSkill(w, r, owner, &skillID)
	}
}

func (s *Server) SetPluginSkillEnabled(w http.ResponseWriter, r *http.Request, pluginId, skillID openapi_types.UUID) {
	if owner, ok := s.pluginOwner(w, r, pluginId); ok {
		s.setSkillEnabled(w, r, owner, skillID)
	}
}

func (s *Server) DeletePluginSkill(w http.ResponseWriter, r *http.Request, pluginId, skillID openapi_types.UUID) {
	if owner, ok := s.pluginOwner(w, r, pluginId); ok {
		s.deleteSkill(w, r, owner, skillID)
	}
}
