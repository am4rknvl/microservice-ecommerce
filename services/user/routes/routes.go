package routes

import (
	"playful-marketplace/services/user/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupUserRoutes(api fiber.Router, userHandler *handlers.UserHandler, cfg *config.Config) {
	users := api.Group("/users", middleware.AuthMiddleware(cfg))

	// User profile routes
	users.Get("/search", userHandler.SearchUsers)
	users.Get("/:id", userHandler.GetUserProfile)
	users.Put("/:id", userHandler.UpdateUserProfile)
	users.Get("/:id/xp-history", userHandler.GetXPHistory)
	users.Get("/:id/badges", userHandler.GetUserBadges)
	users.Get("/:id/stats", userHandler.GetUserStats)
}
