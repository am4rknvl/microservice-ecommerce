package routes

import (
	"playful-marketplace/services/auth/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupAuthRoutes(api fiber.Router, authHandler *handlers.AuthHandler, cfg *config.Config) {
	auth := api.Group("/auth")

	// Public routes
	auth.Post("/signup", authHandler.Signup)
	auth.Post("/request-otp", authHandler.RequestOTP)
	auth.Post("/login", authHandler.Login)

	// Protected routes
	protected := auth.Group("", middleware.AuthMiddleware(cfg))
	protected.Post("/logout", authHandler.Logout)
	protected.Get("/verify", authHandler.VerifyToken)
}
