package middleware

import (
	"strings"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/redis"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
)

func AuthMiddleware(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return utils.UnauthorizedResponse(c, "Authorization header required")
		}

		token := utils.ExtractTokenFromHeader(authHeader)
		if token == "" {
			return utils.UnauthorizedResponse(c, "Invalid authorization header format")
		}

		// Validate JWT
		claims, err := utils.ValidateJWT(token, cfg)
		if err != nil {
			return utils.UnauthorizedResponse(c, "Invalid token")
		}

		// Check if session exists in Redis
		session, err := redis.GetSession(token)
		if err != nil {
			return utils.UnauthorizedResponse(c, "Session expired or invalid")
		}

		// Store user info in context
		c.Locals("user_id", claims.UserID)
		c.Locals("user_phone", claims.Phone)
		c.Locals("user_role", claims.Role)
		c.Locals("session", session)

		return c.Next()
	}
}

func RoleMiddleware(allowedRoles ...models.UserRole) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userRole, ok := c.Locals("user_role").(models.UserRole)
		if !ok {
			return utils.UnauthorizedResponse(c, "User role not found")
		}

		for _, role := range allowedRoles {
			if userRole == role {
				return c.Next()
			}
		}

		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions", nil)
	}
}

func CORSMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")

		if c.Method() == "OPTIONS" {
			return c.SendStatus(fiber.StatusOK)
		}

		return c.Next()
	}
}

func LoggingMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Simple logging - in production, use a proper logging library
		method := c.Method()
		path := c.Path()
		ip := c.IP()
		
		err := c.Next()
		
		status := c.Response().StatusCode()
		
		// Log format: METHOD PATH IP STATUS
		if !strings.HasPrefix(path, "/health") { // Don't log health checks
			fiber.DefaultLogger.Printf("%s %s %s %d\n", method, path, ip, status)
		}
		
		return err
	}
}
