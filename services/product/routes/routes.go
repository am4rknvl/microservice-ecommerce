package routes

import (
	"playful-marketplace/services/product/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"
	"playful-marketplace/shared/models"

	"github.com/gofiber/fiber/v2"
)

func SetupProductRoutes(api fiber.Router, productHandler *handlers.ProductHandler, cfg *config.Config) {
	products := api.Group("/products")

	// Public routes
	products.Get("/", productHandler.GetProducts)
	products.Get("/search", productHandler.SearchProducts)
	products.Get("/categories", productHandler.GetCategories)
	products.Get("/:id", productHandler.GetProduct)

	// Protected routes
	protected := products.Group("", middleware.AuthMiddleware(cfg))
	
	// Seller-only routes
	sellerOnly := protected.Group("", middleware.RoleMiddleware(models.RoleSeller))
	sellerOnly.Post("/", productHandler.CreateProduct)
	sellerOnly.Put("/:id", productHandler.UpdateProduct)
	sellerOnly.Delete("/:id", productHandler.DeleteProduct)
}
