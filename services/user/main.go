package main

import (
	"log"

	"playful-marketplace/services/user/handlers"
	"playful-marketplace/services/user/routes"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/middleware"
	"playful-marketplace/shared/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// @title Playful Marketplace User Service API
// @version 1.0
// @description User management service for the Playful Marketplace
// @host localhost:8002
// @BasePath /api/v1
func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Connect to Redis
	if err := redis.Connect(cfg); err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName: "Playful Marketplace User Service",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"message": "Internal Server Error",
				"error":   err.Error(),
			})
		},
	})

	// Middleware
	app.Use(recover.New())
	app.Use(middleware.CORSMiddleware())
	app.Use(middleware.LoggingMiddleware())

	// Initialize handlers
	userHandler := handlers.NewUserHandler(cfg)

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "user",
		})
	})

	// API routes
	api := app.Group("/api/v1")
	routes.SetupUserRoutes(api, userHandler, cfg)

	// Start server
	port := cfg.Server.Port
	if port == "" {
		port = "8002" // Default port for user service
	}

	log.Printf("User Service starting on port %s", port)
	log.Fatal(app.Listen(":" + port))
}
