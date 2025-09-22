package routes

import (
	"playful-marketplace/services/order/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"
	"playful-marketplace/shared/models"

	"github.com/gofiber/fiber/v2"
)

func SetupOrderRoutes(api fiber.Router, orderHandler *handlers.OrderHandler, cfg *config.Config) {
	orders := api.Group("/orders", middleware.AuthMiddleware(cfg))

	// Order routes
	orders.Post("/", orderHandler.CreateOrder)
	orders.Get("/:id", orderHandler.GetOrder)
	
	// Seller-only routes
	sellerOnly := orders.Group("", middleware.RoleMiddleware(models.RoleSeller))
	sellerOnly.Put("/:id/status", orderHandler.UpdateOrderStatus)

	// User orders
	users := api.Group("/users", middleware.AuthMiddleware(cfg))
	users.Get("/:id/orders", orderHandler.GetUserOrders)
}
