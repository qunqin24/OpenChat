package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/httpx"
	"github.com/qunqin/openchat/apps/server/internal/webui"
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

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(httpx.RequestLogger)
	// No global timeout: CreateMessage streams for as long as the model takes.
	r.Use(middleware.Compress(5, "application/json"))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders: []string{"X-Request-Id"},
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

		// ---- guest ------------------------------------------------------
		// No session: a guest has no account. The status is always there, for
		// the sign-in page to read; the rest answers 404 while the
		// administrator has guest mode off. Every endpoint that could cost
		// something is counted against the guest limits (guest.go).
		r.Get("/guest/status", wrapper.GetGuestStatus)
		r.Group(func(r chi.Router) {
			r.Use(s.requireGuestMode)

			// Stopping a run is allowed to an address banned meanwhile: the run
			// is already paid for, and ending it early spends less.
			r.Post("/guest/runs/{runId}/cancel", wrapper.CancelGuestRun)

			// An address banned by content moderation is refused everywhere
			// else, with 403 guest_banned (moderation_admin.go).
			r.Group(func(r chi.Router) {
				r.Use(s.rejectBannedGuests)

				r.Get("/guest/config", wrapper.GetGuestConfig)
				r.Post("/guest/runs", wrapper.GuestRun)
				r.Post("/guest/title", wrapper.GuestTitle)
				r.Post("/guest/compaction", wrapper.GuestCompact)
				r.Get("/guest/favicons/{domain}", wrapper.GetGuestFavicon)
				r.Get("/guest/images", wrapper.GetGuestImage)
			})
		})

		// ---- signed URL -------------------------------------------------
		// Where the local storage driver's upload and download URLs lead. Always
		// mounted, because the driver can be switched on while the server runs;
		// the handlers answer 404 when there is no local store. No session is
		// asked for. The URL's signature, which binds the method, the file and
		// an expiry, is the authorization — the same bargain as an S3 presigned
		// URL, and why these are not in the group below.
		r.Put("/files/content/*", s.PutFileContent)
		r.Get("/files/content/*", s.GetFileContent)

		// ---- signed in --------------------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.Require)

			r.Post("/auth/logout", wrapper.Logout)
			r.Get("/auth/session", wrapper.GetSession)
			r.Patch("/auth/me", wrapper.UpdateMe)
			r.Post("/auth/password", wrapper.ChangePassword)

			// The caller's own picture, and their data as a whole.
			r.Put("/me/avatar", wrapper.UpdateMyAvatar)
			r.Delete("/me/avatar", wrapper.DeleteMyAvatar)
			r.Get("/me/export", wrapper.ExportMyConversations)
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
			r.Post("/me/memory-events/{eventId}/undo", wrapper.UndoMemoryEvent)

			r.Get("/conversations", wrapper.ListConversations)
			r.Post("/conversations", wrapper.CreateConversation)
			r.Get("/conversations/{conversationId}", wrapper.GetConversation)
			r.Patch("/conversations/{conversationId}", wrapper.UpdateConversation)
			r.Delete("/conversations/{conversationId}", wrapper.DeleteConversation)
			r.Post("/conversations/{conversationId}/title", wrapper.GenerateConversationTitle)
			r.Get("/conversations/{conversationId}/context", wrapper.GetConversationContext)
			r.Get("/conversations/{conversationId}/compaction", wrapper.GetCompaction)
			r.Post("/conversations/{conversationId}/compaction", wrapper.CompactConversation)
			r.Delete("/conversations/{conversationId}/compaction", wrapper.UndoCompaction)
			r.Get("/conversations/running", wrapper.ListRunningConversations)
			r.Get("/conversations/{conversationId}/messages", wrapper.ListMessages)
			r.Post("/conversations/{conversationId}/messages", wrapper.CreateMessage)
			r.Get("/conversations/{conversationId}/events", wrapper.StreamConversationEvents)
			r.Post("/conversations/{conversationId}/messages/{messageId}/edit", wrapper.EditMessage)
			r.Post("/conversations/{conversationId}/regenerate", wrapper.RegenerateAnswer)
			r.Post("/conversations/{conversationId}/tool-calls/{callId}/answer", wrapper.AnswerQuestion)
			r.Post("/conversations/{conversationId}/runs/{runId}/cancel", wrapper.CancelRun)
			r.Get("/conversations/{conversationId}/images", wrapper.GetConversationImage)

			// Any signed-in user: these are the models a conversation can use,
			// not the channels behind them.
			r.Get("/models", wrapper.ListChatModels)
			r.Get("/features", wrapper.GetChatFeatures)
			r.Get("/favicons/{domain}", wrapper.GetFavicon)

			r.Post("/files", wrapper.CreateUpload)

			// ---- admin ---------------------------------------------------
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)

				r.Get("/admin/dashboard", wrapper.GetAdminDashboard)
				r.Get("/admin/system", wrapper.GetSystemSettings)
				r.Get("/admin/web-search", wrapper.GetWebSearchSettings)
				r.Patch("/admin/web-search", wrapper.UpdateWebSearchSettings)
				r.Post("/admin/web-search/test", wrapper.TestWebSearch)
				r.Get("/admin/storage", wrapper.GetStorageSettings)
				r.Patch("/admin/storage", wrapper.UpdateStorageSettings)
				r.Post("/admin/storage/test", wrapper.TestStorage)
				r.Patch("/admin/system", wrapper.UpdateSystemSettings)

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
				r.Get("/admin/moderation/bans", wrapper.ListGuestBans)
				r.Post("/admin/moderation/bans", wrapper.CreateGuestBan)
				r.Delete("/admin/moderation/bans", wrapper.DeleteGuestBan)

				r.Get("/admin/usage-logs", wrapper.ListUsageLogs)
				r.Get("/admin/users", wrapper.ListUsers)
				r.Post("/admin/users", wrapper.CreateUser)
				r.Post("/admin/users/bulk", wrapper.BulkUpdateUsers)
				r.Patch("/admin/users/{userId}", wrapper.UpdateUser)
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
