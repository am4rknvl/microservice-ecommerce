package handlers

import (
	"fmt"
	"math/rand"
	"time"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/redis"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	config *config.Config
}

type SignupRequest struct {
	Phone string           `json:"phone" validate:"required"`
	Name  string           `json:"name" validate:"required"`
	Email string           `json:"email"`
	Role  models.UserRole  `json:"role" validate:"required"`
}

type LoginRequest struct {
	Phone string `json:"phone" validate:"required"`
	OTP   string `json:"otp" validate:"required"`
}

type OTPRequest struct {
	Phone string `json:"phone" validate:"required"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		config: cfg,
	}
}

// @Summary Sign up a new user
// @Description Create a new user account
// @Tags auth
// @Accept json
// @Produce json
// @Param request body SignupRequest true "Signup request"
// @Success 201 {object} utils.Response{data=AuthResponse}
// @Failure 400 {object} utils.Response
// @Failure 409 {object} utils.Response
// @Router /auth/signup [post]
func (h *AuthHandler) Signup(c *fiber.Ctx) error {
	var req SignupRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Validate required fields
	if req.Phone == "" || req.Name == "" || req.Role == "" {
		return utils.ValidationErrorResponse(c, "Phone, name, and role are required")
	}

	// Validate role
	if req.Role != models.RoleBuyer && req.Role != models.RoleSeller {
		return utils.ValidationErrorResponse(c, "Role must be 'buyer' or 'seller'")
	}

	// Check if user already exists
	var existingUser models.User
	if err := database.DB.Where("phone = ?", req.Phone).First(&existingUser).Error; err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, "User with this phone number already exists", nil)
	}

	// Create new user
	user := models.User{
		BaseModel: models.BaseModel{
			ID: uuid.New(),
		},
		Phone:    req.Phone,
		Name:     req.Name,
		Email:    req.Email,
		Role:     req.Role,
		Level:    models.LevelBronze,
		TotalXP:  0,
		IsActive: true,
	}

	if err := database.DB.Create(&user).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create user", err)
	}

	// Generate JWT token
	token, err := utils.GenerateJWT(&user, h.config)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to generate token", err)
	}

	// Create session
	session := &models.Session{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(h.config.JWT.ExpiryHours) * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := redis.SetSession(session); err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create session", err)
	}

	// Award early bird badge if user is among first 100
	go h.checkEarlyBirdBadge(&user)

	response := AuthResponse{
		Token: token,
		User:  &user,
	}

	return utils.SuccessResponse(c, "User created successfully", response)
}

// @Summary Request OTP for login
// @Description Send OTP to user's phone for authentication
// @Tags auth
// @Accept json
// @Produce json
// @Param request body OTPRequest true "OTP request"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /auth/request-otp [post]
func (h *AuthHandler) RequestOTP(c *fiber.Ctx) error {
	var req OTPRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	if req.Phone == "" {
		return utils.ValidationErrorResponse(c, "Phone number is required")
	}

	// Check if user exists
	var user models.User
	if err := database.DB.Where("phone = ?", req.Phone).First(&user).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Generate mock OTP (in production, integrate with SMS service)
	otp := h.generateMockOTP()

	// Store OTP in Redis with 5-minute expiration
	otpKey := fmt.Sprintf("otp:%s", req.Phone)
	if err := redis.Set(otpKey, otp, 5*time.Minute); err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to store OTP", err)
	}

	// In production, send OTP via SMS
	// For now, return it in response (ONLY FOR DEVELOPMENT)
	return utils.SuccessResponse(c, "OTP sent successfully", fiber.Map{
		"otp": otp, // Remove this in production
		"message": "OTP sent to your phone number",
	})
}

// @Summary Login with phone and OTP
// @Description Authenticate user with phone number and OTP
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login request"
// @Success 200 {object} utils.Response{data=AuthResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	if req.Phone == "" || req.OTP == "" {
		return utils.ValidationErrorResponse(c, "Phone and OTP are required")
	}

	// Verify OTP
	otpKey := fmt.Sprintf("otp:%s", req.Phone)
	var storedOTP string
	if err := redis.Get(otpKey, &storedOTP); err != nil {
		return utils.UnauthorizedResponse(c, "Invalid or expired OTP")
	}

	if storedOTP != req.OTP {
		return utils.UnauthorizedResponse(c, "Invalid OTP")
	}

	// Get user
	var user models.User
	if err := database.DB.Where("phone = ?", req.Phone).First(&user).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Update last login
	now := time.Now()
	user.LastLoginAt = &now
	database.DB.Save(&user)

	// Generate JWT token
	token, err := utils.GenerateJWT(&user, h.config)
	if err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to generate token", err)
	}

	// Create session
	session := &models.Session{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(h.config.JWT.ExpiryHours) * time.Hour),
		CreatedAt: time.Now(),
	}

	if err := redis.SetSession(session); err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create session", err)
	}

	// Delete used OTP
	redis.Delete(otpKey)

	response := AuthResponse{
		Token: token,
		User:  &user,
	}

	return utils.SuccessResponse(c, "Login successful", response)
}

// @Summary Logout user
// @Description Invalidate user session
// @Tags auth
// @Security BearerAuth
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c *fiber.Ctx) error {
	// Get session from context (set by auth middleware)
	session, ok := c.Locals("session").(*models.Session)
	if !ok {
		return utils.UnauthorizedResponse(c, "Session not found")
	}

	// Delete session from Redis
	if err := redis.DeleteSession(session.Token); err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to logout", err)
	}

	return utils.SuccessResponse(c, "Logout successful", nil)
}

// @Summary Verify token
// @Description Verify if the provided token is valid
// @Tags auth
// @Security BearerAuth
// @Success 200 {object} utils.Response{data=models.User}
// @Failure 401 {object} utils.Response
// @Router /auth/verify [get]
func (h *AuthHandler) VerifyToken(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	// Get user details
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	return utils.SuccessResponse(c, "Token is valid", user)
}

func (h *AuthHandler) generateMockOTP() string {
	// Generate 6-digit OTP
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

func (h *AuthHandler) checkEarlyBirdBadge(user *models.User) {
	// Count total users
	var userCount int64
	database.DB.Model(&models.User{}).Count(&userCount)

	if userCount <= 100 {
		// Award early bird badge
		var badge models.Badge
		if err := database.DB.Where("type = ?", models.BadgeEarlyBird).First(&badge).Error; err == nil {
			// Check if user already has this badge
			var existingBadge models.UserBadge
			if err := database.DB.Where("user_id = ? AND badge_id = ?", user.ID, badge.ID).First(&existingBadge).Error; err != nil {
				// Award badge
				userBadge := models.UserBadge{
					BaseModel: models.BaseModel{ID: uuid.New()},
					UserID:    user.ID,
					BadgeID:   badge.ID,
					EarnedAt:  time.Now(),
				}
				database.DB.Create(&userBadge)

				// Award XP
				if badge.XPReward > 0 {
					h.awardXP(user.ID, badge.XPReward, "Early Bird Badge")
				}
			}
		}
	}
}

func (h *AuthHandler) awardXP(userID uuid.UUID, amount int, reason string) {
	// Create XP transaction
	xpTransaction := models.XPTransaction{
		BaseModel: models.BaseModel{ID: uuid.New()},
		UserID:    userID,
		Amount:    amount,
		Reason:    reason,
	}
	database.DB.Create(&xpTransaction)

	// Update user's total XP
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("total_xp", database.DB.Raw("total_xp + ?", amount))

	// Check for level up
	var user models.User
	if err := database.DB.First(&user, userID).Error; err == nil {
		newLevel := h.calculateLevel(user.TotalXP)
		if newLevel != user.Level {
			database.DB.Model(&user).Update("level", newLevel)
		}
	}
}

func (h *AuthHandler) calculateLevel(xp int) models.UserLevel {
	if xp >= 5000 {
		return models.LevelPlatinum
	} else if xp >= 1500 {
		return models.LevelGold
	} else if xp >= 500 {
		return models.LevelSilver
	}
	return models.LevelBronze
}
