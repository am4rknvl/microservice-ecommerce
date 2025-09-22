package routes

import (
	"playful-marketplace/services/gamification/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupGamificationRoutes(api fiber.Router, gamificationHandler *handlers.GamificationHandler, cfg *config.Config) {
	gamify := api.Group("/gamify", middleware.AuthMiddleware(cfg))

	// XP routes
	gamify.Post("/xp", gamificationHandler.AddXP)
	gamify.Get("/xp/:userId", gamificationHandler.GetUserXP)

	// Badge routes
	gamify.Get("/badges/:userId", gamificationHandler.GetUserBadges)
	gamify.Post("/badges/check", gamificationHandler.CheckAndAwardBadges)

	// Level routes
	gamify.Get("/level/:userId", gamificationHandler.GetUserLevel)
	gamify.Post("/level/update/:userId", gamificationHandler.UpdateUserLevel)

	// Leaderboard routes
	gamify.Get("/leaderboard/buyers", gamificationHandler.GetBuyerLeaderboard)
	gamify.Get("/leaderboard/sellers", gamificationHandler.GetSellerLeaderboard)
}
