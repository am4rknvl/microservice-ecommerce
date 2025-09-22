package handlers

import (
	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type UserHandler struct {
	config *config.Config
}

type UpdateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserProfileResponse struct {
	*models.User
	BadgeCount int                    `json:"badge_count"`
	Badges     []models.Badge         `json:"badges"`
	XPHistory  []models.XPTransaction `json:"xp_history"`
}

func NewUserHandler(cfg *config.Config) *UserHandler {
	return &UserHandler{
		config: cfg,
	}
}

// @Summary Get user profile
// @Description Get user profile with XP, badges, and level information
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response{data=UserProfileResponse}
// @Failure 404 {object} utils.Response
// @Router /users/{id} [get]
func (h *UserHandler) GetUserProfile(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	// Get user with relationships
	var user models.User
	if err := database.DB.Preload("Badges.Badge").First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Get user badges
	var userBadges []models.UserBadge
	database.DB.Preload("Badge").Where("user_id = ?", userID).Find(&userBadges)

	badges := make([]models.Badge, len(userBadges))
	for i, ub := range userBadges {
		badges[i] = ub.Badge
	}

	// Get XP history (last 10 transactions)
	var xpHistory []models.XPTransaction
	database.DB.Where("user_id = ?", userID).Order("created_at DESC").Limit(10).Find(&xpHistory)

	response := UserProfileResponse{
		User:       &user,
		BadgeCount: len(badges),
		Badges:     badges,
		XPHistory:  xpHistory,
	}

	return utils.SuccessResponse(c, "User profile retrieved successfully", response)
}

// @Summary Update user profile
// @Description Update user profile information
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param request body UpdateUserRequest true "Update user request"
// @Success 200 {object} utils.Response{data=models.User}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /users/{id} [put]
func (h *UserHandler) UpdateUserProfile(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	// Check if user can update this profile (must be the same user)
	currentUserID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok || currentUserID != userID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only update your own profile", nil)
	}

	var req UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Get user
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Update fields
	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	// Save changes
	if err := database.DB.Save(&user).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to update user", err)
	}

	return utils.SuccessResponse(c, "User profile updated successfully", user)
}

// @Summary Get user XP history
// @Description Get detailed XP transaction history for a user
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param limit query int false "Number of transactions to return" default(20)
// @Param offset query int false "Number of transactions to skip" default(0)
// @Success 200 {object} utils.Response{data=[]models.XPTransaction}
// @Failure 404 {object} utils.Response
// @Router /users/{id}/xp-history [get]
func (h *UserHandler) GetXPHistory(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	// Parse query parameters
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	if limit > 100 {
		limit = 100 // Cap at 100 for performance
	}

	// Check if user exists
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Get XP history
	var xpHistory []models.XPTransaction
	if err := database.DB.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&xpHistory).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to get XP history", err)
	}

	return utils.SuccessResponse(c, "XP history retrieved successfully", xpHistory)
}

// @Summary Get user badges
// @Description Get all badges earned by a user
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response{data=[]models.UserBadge}
// @Failure 404 {object} utils.Response
// @Router /users/{id}/badges [get]
func (h *UserHandler) GetUserBadges(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	// Check if user exists
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Get user badges with badge details
	var userBadges []models.UserBadge
	if err := database.DB.Preload("Badge").Where("user_id = ?", userID).Find(&userBadges).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to get user badges", err)
	}

	return utils.SuccessResponse(c, "User badges retrieved successfully", userBadges)
}

// @Summary Get user statistics
// @Description Get comprehensive statistics for a user
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 404 {object} utils.Response
// @Router /users/{id}/stats [get]
func (h *UserHandler) GetUserStats(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	// Get user
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Get additional statistics
	var orderCount int64
	var productCount int64
	var badgeCount int64

	database.DB.Model(&models.Order{}).Where("buyer_id = ?", userID).Count(&orderCount)
	database.DB.Model(&models.Product{}).Where("seller_id = ?", userID).Count(&productCount)
	database.DB.Model(&models.UserBadge{}).Where("user_id = ?", userID).Count(&badgeCount)

	// Calculate XP to next level
	var xpToNextLevel int
	switch user.Level {
	case models.LevelBronze:
		xpToNextLevel = 500 - user.TotalXP
	case models.LevelSilver:
		xpToNextLevel = 1500 - user.TotalXP
	case models.LevelGold:
		xpToNextLevel = 5000 - user.TotalXP
	case models.LevelPlatinum:
		xpToNextLevel = 0 // Max level
	}

	if xpToNextLevel < 0 {
		xpToNextLevel = 0
	}

	stats := map[string]interface{}{
		"user_id":         user.ID,
		"name":            user.Name,
		"level":           user.Level,
		"total_xp":        user.TotalXP,
		"xp_to_next_level": xpToNextLevel,
		"total_spent":     user.TotalSpent,
		"total_sales":     user.TotalSales,
		"order_count":     orderCount,
		"product_count":   productCount,
		"badge_count":     badgeCount,
		"member_since":    user.CreatedAt,
		"last_login":      user.LastLoginAt,
	}

	return utils.SuccessResponse(c, "User statistics retrieved successfully", stats)
}

// @Summary Search users
// @Description Search users by name or phone
// @Tags users
// @Security BearerAuth
// @Param q query string true "Search query"
// @Param limit query int false "Number of users to return" default(10)
// @Param offset query int false "Number of users to skip" default(0)
// @Success 200 {object} utils.Response{data=[]models.User}
// @Failure 400 {object} utils.Response
// @Router /users/search [get]
func (h *UserHandler) SearchUsers(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return utils.ValidationErrorResponse(c, "Search query is required")
	}

	limit := c.QueryInt("limit", 10)
	offset := c.QueryInt("offset", 0)

	if limit > 50 {
		limit = 50 // Cap at 50 for performance
	}

	var users []models.User
	if err := database.DB.Where("name ILIKE ? OR phone ILIKE ?", "%"+query+"%", "%"+query+"%").
		Select("id, name, phone, role, level, total_xp, created_at").
		Limit(limit).
		Offset(offset).
		Find(&users).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to search users", err)
	}

	return utils.SuccessResponse(c, "Users found successfully", users)
}
