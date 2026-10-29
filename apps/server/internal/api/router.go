package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/qunqin24/openchat/apps/server/internal/auth"
	"github.com/qunqin24/openchat/apps/server/internal/config"
	"github.com/qunqin24/openchat/apps/server/internal/httpx"
	"github.com/qunqin24/openchat/apps/server/internal/webui"
)

// NewRouter mounts the generated handlers under /api/v1 behind the standard
// middleware stack.
//
// Routes are registered by hand rather than through HandlerFromMux so that each
// one's protection level is visible here. The generated wrapper still does all
// the parameter parsing; only the mounting is explicit. A new endpoint added to
// the contract therefore has to be given a group deliberately — it cannot
// inherit "public" by being forgotten.
func NewRouter(cfg *config.Config, s *Server, mw *auth.Middleware) http.Handler {
	r := chi.NewRouter()
	// A disabled account's session is told why, not signed out (account_disabled.go).
	mw.OnDisabled(s.failDisabled)

	r.Use(httpx.RequestID)
	r.Use(buildHeader(webui.BuildID()))
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(httpx.RequestLogger)
	// No global timeout: the conversation event stream (GET .../events) stays
	// open for as long as a run takes.
	r.Use(middleware.Compress(5, "application/json"))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders: []string{"X-Request-Id", buildHeaderName},
		// Required for the session cookie to survive a cross-origin request.
		AllowCredentials: true,
		MaxAge:           int((12 * time.Hour).Seconds()),
	}))

	wrapper := ServerInterfaceWrapper{
		Handler: s,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "invalid_request", err.Error()))
		},
	}

	r.Route("/api/v1", func(r chi.Router) {
		// ---- public -----------------------------------------------------
		r.Get("/healthz", wrapper.GetHealth)
		r.Get("/auth/options", wrapper.GetAuthOptions)
		r.Post("/auth/register", wrapper.Register)
		r.Post("/auth/login", wrapper.Login)
		// A passkey sign-in: a challenge, then the answer (passkeys.go).
		r.Post("/auth/passkey/options", wrapper.BeginPasskeySignIn)
		r.Post("/auth/passkey", wrapper.PasskeySignIn)

		// ---- signed URL -------------------------------------------------
		// Where the local storage driver's upload and download URLs lead. Always
		// mounted, because the driver can be switched on while the server runs;
		// the handlers answer 404 when there is no local store. No session is
		// asked for. The URL's signature, which binds the method, the file and
		// an expiry, is the authorization — the same bargain as an S3 presigned
		// URL, and why these are not in the group below.
		r.Put("/files/content/*", s.PutFileContent)
		r.Get("/files/content/*", s.GetFileContent)

		// Signing out needs only the cookie, not a live account: a disabled
		// account's session, which every other route refuses, can still end.
		r.Post("/auth/logout", wrapper.Logout)

		// ---- signed in --------------------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.Require)

			r.Get("/auth/session", wrapper.GetSession)
			r.Patch("/auth/me", wrapper.UpdateMe)
			r.Post("/auth/password", wrapper.ChangePassword)
			r.Get("/auth/passkeys", wrapper.ListPasskeys)
			r.Post("/auth/passkeys/options", wrapper.BeginPasskeyRegistration)
			r.Post("/auth/passkeys", wrapper.AddPasskey)
			r.Patch("/auth/passkeys/{passkeyId}", wrapper.RenamePasskey)
			r.Delete("/auth/passkeys/{passkeyId}", wrapper.DeletePasskey)

			// The caller's own picture, and their data as a whole.
			r.Put("/me/avatar", wrapper.UpdateMyAvatar)
			r.Delete("/me/avatar", wrapper.DeleteMyAvatar)
			r.Get("/me/export", wrapper.ExportMyConversations)
			r.Get("/me/storage", wrapper.GetMyStorage)
			r.Delete("/me/conversations", wrapper.DeleteAllMyConversations)

			// Personal memory: always the caller's own, from the session.
			r.Get("/me/personalization", wrapper.GetPersonalization)
			r.Patch("/me/personalization", wrapper.UpdatePersonalization)
			r.Get("/me/memories", wrapper.ListMemories)
			r.Post("/me/memories", wrapper.CreateMemory)
			r.Delete("/me/memories", wrapper.ClearMemories)
			r.Patch("/me/memories/{memoryId}", wrapper.UpdateMemory)
			r.Delete("/me/memories/{memoryId}", wrapper.DeleteMemory)
			r.Get("/me/memory-events", wrapper.ListMemoryEvents)

			// Writing blocks: the caller's own (the queries name the owner).
			r.Get("/conversations/{conversationId}/documents", wrapper.ListConversationDocuments)
			r.Get("/documents/{documentId}", wrapper.GetDocument)
			r.Patch("/documents/{documentId}", wrapper.UpdateDocument)

			// Scheduled tasks: always the caller's own; 404 while switched off.
			r.Get("/tasks", wrapper.ListTasks)
			r.Post("/tasks", wrapper.CreateTask)
			r.Patch("/tasks/{taskId}", wrapper.UpdateTask)
			r.Delete("/tasks/{taskId}", wrapper.DeleteTask)
			r.Post("/tasks/{taskId}/run", wrapper.RunTask)
			r.Post("/me/memory-events/{eventId}/undo", wrapper.UndoMemoryEvent)

			r.Get("/conversations", wrapper.ListConversations)
			r.Post("/conversations", wrapper.CreateConversation)
			r.Get("/conversations/{conversationId}", wrapper.GetConversation)
			r.Patch("/conversations/{conversationId}", wrapper.UpdateConversation)
			r.Delete("/conversations/{conversationId}", wrapper.DeleteConversation)
			r.Post("/conversations/{conversationId}/title", wrapper.GenerateConversationTitle)
			r.Get("/conversations/{conversationId}/context", wrapper.GetConversationContext)
			r.Get("/conversations/{conversationId}/compaction", wrapper.GetCompaction)
			r.Delete("/conversations/{conversationId}/compaction", wrapper.UndoCompaction)
			r.Get("/conversations/running", wrapper.ListRunningConversations)
			r.Get("/conversations/{conversationId}/messages", wrapper.ListMessages)
			r.Post("/conversations/{conversationId}/messages", wrapper.CreateMessage)
			r.Get("/conversations/{conversationId}/events", wrapper.StreamConversationEvents)
			r.Post("/conversations/{conversationId}/messages/{messageId}/edit", wrapper.EditMessage)
			r.Post("/conversations/{conversationId}/regenerate", wrapper.RegenerateAnswer)
			r.Post("/conversations/{conversationId}/tool-calls/{callId}/answer", wrapper.AnswerQuestion)
			r.Post("/conversations/{conversationId}/runs/{runId}/cancel", wrapper.CancelRun)
			// A conversation's sub-agents: read, stopped, resumed, moved to the
			// background - never written to (subagents_http.go).
			r.Get("/conversations/{conversationId}/subagents", wrapper.ListSubagents)
			r.Get("/conversations/{conversationId}/subagents/{subagentId}", wrapper.GetSubagent)
			r.Post("/conversations/{conversationId}/subagents/{subagentId}/stop", wrapper.StopSubagent)
			r.Post("/conversations/{conversationId}/subagents/{subagentId}/resume", wrapper.ResumeSubagent)
			r.Post("/conversations/{conversationId}/subagents/{subagentId}/background", wrapper.BackgroundSubagent)
			r.Get("/conversations/{conversationId}/images", wrapper.GetConversationImage)

			// Any signed-in user: these are the models a conversation can use,
			// not the channels behind them.
			r.Get("/models", wrapper.ListChatModels)
			r.Get("/features", wrapper.GetChatFeatures)
			r.Get("/favicons/{domain}", wrapper.GetFavicon)
			// Pictures answers describe, found and served here (pictures.go).
			r.Get("/images/search", wrapper.SearchImage)
			r.Get("/pictures/{pictureId}", wrapper.GetPicture)

			r.Post("/files", wrapper.CreateUpload)
			// The library: the caller's own sent files (ListLibraryFiles).
			r.Get("/files", wrapper.ListFiles)
			r.Delete("/files/{fileId}", wrapper.DeleteFile)
			r.Get("/files/{fileId}/download", wrapper.GetFileDownload)
			r.Get("/files/{fileId}/text", wrapper.GetFileText)

			// ---- admin ---------------------------------------------------
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)

				r.Get("/admin/dashboard", wrapper.GetAdminDashboard)
				// The 轨迹 view: the administrator's own conversations only
				// (the query names the owner), so it reads nobody else's.
				r.Get("/conversations/{conversationId}/trace", wrapper.GetConversationTrace)
				r.Get("/admin/system", wrapper.GetSystemSettings)
				r.Get("/admin/version", wrapper.GetVersionInfo)
				r.Post("/admin/version/check", wrapper.CheckForUpdate)
				r.Get("/admin/authentication/passkeys", wrapper.GetPasskeySettings)
				r.Patch("/admin/authentication/passkeys", wrapper.UpdatePasskeySettings)
				r.Get("/admin/web-search", wrapper.GetWebSearchSettings)
				r.Patch("/admin/web-search", wrapper.UpdateWebSearchSettings)
				r.Post("/admin/web-search/test", wrapper.TestWebSearch)
				r.Get("/admin/image-search", wrapper.GetImageSearchSettings)
				r.Patch("/admin/image-search", wrapper.UpdateImageSearchSettings)
				r.Post("/admin/image-search/test", wrapper.TestImageSearch)
				r.Get("/admin/storage", wrapper.GetStorageSettings)
				r.Patch("/admin/storage", wrapper.UpdateStorageSettings)
				r.Get("/admin/storage/quota", wrapper.GetStorageQuota)
				r.Patch("/admin/storage/quota", wrapper.UpdateStorageQuota)
				r.Post("/admin/storage/test", wrapper.TestStorage)
				r.Patch("/admin/system", wrapper.UpdateSystemSettings)
				r.Get("/admin/subagents", wrapper.GetSubagentSettings)
				r.Patch("/admin/subagents", wrapper.UpdateSubagentSettings)

				r.Get("/admin/catalog", wrapper.GetCatalogStatus)
				r.Post("/admin/catalog", wrapper.RefreshCatalog)
				r.Get("/admin/catalog/providers", wrapper.ListCatalogProviders)
				r.Get("/admin/catalog/models", wrapper.ListCatalogModels)

				r.Get("/admin/channel-types", wrapper.ListChannelTypes)
				r.Get("/admin/channels", wrapper.ListChannels)
				r.Post("/admin/channels", wrapper.CreateChannel)
				r.Post("/admin/channels/models/preview", wrapper.PreviewChannelModels)
				r.Patch("/admin/channels/{channelId}", wrapper.UpdateChannel)
				r.Delete("/admin/channels/{channelId}", wrapper.DeleteChannel)

				// The preview and the refresh both make outbound requests
				// carrying an operator's credential, which is reason enough
				// for them to sit in the admin group rather than anywhere
				// looser.
				r.Get("/admin/channels/{channelId}/models", wrapper.ListChannelModels)
				r.Post("/admin/channels/{channelId}/models", wrapper.AddChannelModel)
				r.Post("/admin/channels/{channelId}/models/refresh", wrapper.RefreshChannelModels)
				r.Patch("/admin/channels/{channelId}/models/{modelId}", wrapper.UpdateChannelModel)
				r.Delete("/admin/channels/{channelId}/models/{modelId}", wrapper.DeleteChannelModel)
				r.Put("/admin/models/{modelId}/settings", wrapper.UpdateModelSettings)
				r.Delete("/admin/models/{modelId}/settings", wrapper.ResetModelSettings)
				r.Post("/admin/models/{modelId}/capacity/reset", wrapper.ResetModelCapacity)

				r.Get("/admin/moderation", wrapper.GetModerationSettings)
				r.Patch("/admin/moderation", wrapper.UpdateModerationSettings)
				r.Get("/admin/moderation/events", wrapper.ListModerationEvents)

				r.Get("/admin/usage-logs", wrapper.ListUsageLogs)
				r.Get("/admin/users", wrapper.ListUsers)
				r.Post("/admin/users", wrapper.CreateUser)
				r.Post("/admin/users/bulk", wrapper.BulkUpdateUsers)
				r.Patch("/admin/users/{userId}", wrapper.UpdateUser)
				r.Delete("/admin/users/{userId}", wrapper.DeleteUser)
			})
		})
	})

	// Unversioned probe for load balancers and container health checks.
	r.Get("/healthz", s.GetHealth)

	// ---- the web app --------------------------------------------------------
	// The catch-all, and deliberately last: it is what answers when nothing
	// above matched, so every API route has already had its say. It serves the
	// SPA embedded in the binary (a placeholder note if there is none), falls
	// back to index.html for client-side routes, and never answers /api/* with
	// anything but a 404. The Compress middleware above leaves it alone: it
	// only compresses application/json, and skips any response that already
	// carries a Content-Encoding, which the precompressed files do.
	r.NotFound(webui.Handler().ServeHTTP)

	return r
}

// buildHeaderName carries the embedded front end's build (webui.BuildID) on
// every response. A page loaded before an upgrade sees a different value on its
// next request - the event stream reconnecting after the restart is one - and
// asks the reader to reload (lib/build-check.ts in the web app).
const buildHeaderName = "X-OpenChat-Build"

func buildHeader(id string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if id == "" {
			// Nothing embedded (development): no build to compare.
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(buildHeaderName, id)
			next.ServeHTTP(w, r)
		})
	}
}
