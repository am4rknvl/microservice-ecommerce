package routes

import (
	"playful-marketplace/services/payment/handlers"
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupPaymentRoutes(api fiber.Router, paymentHandler *handlers.PaymentHandler, cfg *config.Config) {
	payments := api.Group("/payments")

	// Public routes
	payments.Get("/methods", paymentHandler.GetPaymentMethods)

	// Protected routes
	protected := payments.Group("", middleware.AuthMiddleware(cfg))
	protected.Post("/initiate", paymentHandler.InitiatePayment)
	protected.Get("/status/:id", paymentHandler.GetPaymentStatus)
}
