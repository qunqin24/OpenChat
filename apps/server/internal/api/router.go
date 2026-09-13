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
		AllowedMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
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
		r.Post("/auth/register", wrapper.Register)
		r.Post("/auth/login", wrapper.Login)

		// ---- signed in --------------------------------------------------
		r.Group(func(r chi.Router) {
			r.Use(mw.Require)

			r.Post("/auth/logout", wrapper.Logout)
			r.Get("/auth/session", wrapper.GetSession)
			r.Patch("/auth/me", wrapper.UpdateMe)
			r.Post("/auth/password", wrapper.ChangePassword)

			r.Get("/conversations", wrapper.ListConversations)
			r.Post("/conversations", wrapper.CreateConversation)
			r.Get("/conversations/{conversationId}", wrapper.GetConversation)
			r.Patch("/conversations/{conversationId}", wrapper.UpdateConversation)
			r.Delete("/conversations/{conversationId}", wrapper.DeleteConversation)
			r.Get("/conversations/{conversationId}/messages", wrapper.ListMessages)
			r.Post("/conversations/{conversationId}/messages", wrapper.CreateMessage)

			r.Post("/files", wrapper.CreateUpload)

			// ---- admin ---------------------------------------------------
			r.Group(func(r chi.Router) {
				r.Use(mw.RequireAdmin)

				r.Get("/admin/stats", wrapper.GetAdminStats)
				r.Get("/admin/system", wrapper.GetSystemSettings)
				r.Patch("/admin/system", wrapper.UpdateSystemSettings)

				r.Get("/admin/catalog", wrapper.GetCatalogStatus)
				r.Post("/admin/catalog", wrapper.RefreshCatalog)
				r.Get("/admin/catalog/providers", wrapper.ListCatalogProviders)
				r.Get("/admin/catalog/models", wrapper.ListCatalogModels)
				r.Patch("/admin/catalog/models/{providerId}/{modelId}", wrapper.UpdateCatalogModel)

				r.Get("/admin/channels", wrapper.ListChannels)
				r.Post("/admin/channels", wrapper.CreateChannel)
				r.Patch("/admin/channels/{channelId}", wrapper.UpdateChannel)
				r.Delete("/admin/channels/{channelId}", wrapper.DeleteChannel)

				r.Get("/admin/users", wrapper.ListUsers)
				r.Post("/admin/users", wrapper.CreateUser)
				r.Post("/admin/users/bulk", wrapper.BulkUpdateUsers)
				r.Patch("/admin/users/{userId}", wrapper.UpdateUser)
			})
		})
	})

	// Unversioned probe for load balancers and container health checks.
	r.Get("/healthz", s.GetHealth)

	return r
}
