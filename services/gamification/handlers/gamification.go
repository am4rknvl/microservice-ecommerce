package handlers

import (
	"fmt"
	"time"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/redis"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type GamificationHandler struct {
	config *config.Config
}

type AddXPRequest struct {
	UserID    uuid.UUID `json:"user_id" validate:"required"`
	Amount    int       `json:"amount" validate:"required"`
	Reason    string    `json:"reason" validate:"required"`
	Reference string    `json:"reference"`
}

type CheckBadgesRequest struct {
	UserID uuid.UUID `json:"user_id" validate:"required"`
}

type LeaderboardResponse struct {
	Type    string                      `json:"type"`
	Period  string                      `json:"period"`
	Entries []models.LeaderboardEntry   `json:"entries"`
}

func NewGamificationHandler(cfg *config.Config) *GamificationHandler {
	return &GamificationHandler{
		config: cfg,
	}
}

// @Summary Add XP to user
// @Description Award XP points to a user for completing actions
// @Tags gamification
// @Security BearerAuth
// @Param request body AddXPRequest true "Add XP request"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Router /gamify/xp [post]
func (h *GamificationHandler) AddXP(c *fiber.Ctx) error {
	var req AddXPRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	if req.UserID == uuid.Nil || req.Amount == 0 || req.Reason == "" {
		return utils.ValidationErrorResponse(c, "User ID, amount, and reason are required")
	}

	// Get user
	var user models.User
	if err := database.DB.First(&user, req.UserID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Create XP transaction
	xpTransaction := models.XPTransaction{
		BaseModel: models.BaseModel{ID: uuid.New()},
		UserID:    req.UserID,
		Amount:    req.Amount,
		Reason:    req.Reason,
		Reference: req.Reference,
	}

	if err := database.DB.Create(&xpTransaction).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create XP transaction", err)
	}

	// Update user's total XP
	oldXP := user.TotalXP
	newXP := oldXP + req.Amount
	
	if err := database.DB.Model(&user).Update("total_xp", newXP).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to update user XP", err)
	}

	// Check for level up
	oldLevel := h.calculateLevel(oldXP)
	newLevel := h.calculateLevel(newXP)
	leveledUp := newLevel != oldLevel

	if leveledUp {
		database.DB.Model(&user).Update("level", newLevel)
	}

	// Update leaderboards
	go h.updateLeaderboards(&user, newXP)

	response := map[string]interface{}{
		"user_id":     req.UserID,
		"old_xp":      oldXP,
		"new_xp":      newXP,
		"xp_gained":   req.Amount,
		"old_level":   oldLevel,
		"new_level":   newLevel,
		"leveled_up":  leveledUp,
		"transaction": xpTransaction,
	}

	return utils.SuccessResponse(c, "XP added successfully", response)
}

// @Summary Get user XP
// @Description Get current XP and level information for a user
// @Tags gamification
// @Security BearerAuth
// @Param userId path string true "User ID"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 404 {object} utils.Response
// @Router /gamify/xp/{userId} [get]
func (h *GamificationHandler) GetUserXP(c *fiber.Ctx) error {
	userIDParam := c.Params("userId")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	// Calculate XP to next level
	xpToNextLevel := h.calculateXPToNextLevel(user.TotalXP, user.Level)

	response := map[string]interface{}{
		"user_id":          user.ID,
		"total_xp":         user.TotalXP,
		"level":            user.Level,
		"xp_to_next_level": xpToNextLevel,
		"level_progress":   h.calculateLevelProgress(user.TotalXP, user.Level),
	}

	return utils.SuccessResponse(c, "User XP retrieved successfully", response)
}

// @Summary Get user badges
// @Description Get all badges earned by a user
// @Tags gamification
// @Security BearerAuth
// @Param userId path string true "User ID"
// @Success 200 {object} utils.Response{data=[]models.UserBadge}
// @Failure 404 {object} utils.Response
// @Router /gamify/badges/{userId} [get]
func (h *GamificationHandler) GetUserBadges(c *fiber.Ctx) error {
	userIDParam := c.Params("userId")
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
	if err := database.DB.Preload("Badge").Where("user_id = ?", userID).Order("earned_at DESC").Find(&userBadges).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to get user badges", err)
	}

	return utils.SuccessResponse(c, "User badges retrieved successfully", userBadges)
}

// @Summary Check and award badges
// @Description Check if user qualifies for new badges and award them
// @Tags gamification
// @Security BearerAuth
// @Param request body CheckBadgesRequest true "Check badges request"
// @Success 200 {object} utils.Response{data=[]models.UserBadge}
// @Failure 400 {object} utils.Response
// @Router /gamify/badges/check [post]
func (h *GamificationHandler) CheckAndAwardBadges(c *fiber.Ctx) error {
	var req CheckBadgesRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	if req.UserID == uuid.Nil {
		return utils.ValidationErrorResponse(c, "User ID is required")
	}

	// Get user
	var user models.User
	if err := database.DB.First(&user, req.UserID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	newBadges := h.checkAndAwardBadges(&user)

	return utils.SuccessResponse(c, fmt.Sprintf("Checked badges, awarded %d new badges", len(newBadges)), newBadges)
}

// @Summary Get user level
// @Description Get current level information for a user
// @Tags gamification
// @Security BearerAuth
// @Param userId path string true "User ID"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 404 {object} utils.Response
// @Router /gamify/level/{userId} [get]
func (h *GamificationHandler) GetUserLevel(c *fiber.Ctx) error {
	userIDParam := c.Params("userId")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	response := map[string]interface{}{
		"user_id":          user.ID,
		"level":            user.Level,
		"total_xp":         user.TotalXP,
		"xp_to_next_level": h.calculateXPToNextLevel(user.TotalXP, user.Level),
		"level_progress":   h.calculateLevelProgress(user.TotalXP, user.Level),
		"level_info":       h.getLevelInfo(user.Level),
	}

	return utils.SuccessResponse(c, "User level retrieved successfully", response)
}

// @Summary Update user level
// @Description Recalculate and update user level based on current XP
// @Tags gamification
// @Security BearerAuth
// @Param userId path string true "User ID"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 404 {object} utils.Response
// @Router /gamify/level/update/{userId} [post]
func (h *GamificationHandler) UpdateUserLevel(c *fiber.Ctx) error {
	userIDParam := c.Params("userId")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return utils.NotFoundResponse(c, "User not found")
	}

	oldLevel := user.Level
	newLevel := h.calculateLevel(user.TotalXP)
	leveledUp := newLevel != oldLevel

	if leveledUp {
		if err := database.DB.Model(&user).Update("level", newLevel).Error; err != nil {
			return utils.InternalServerErrorResponse(c, "Failed to update user level", err)
		}
	}

	response := map[string]interface{}{
		"user_id":    user.ID,
		"old_level":  oldLevel,
		"new_level":  newLevel,
		"leveled_up": leveledUp,
		"total_xp":   user.TotalXP,
	}

	return utils.SuccessResponse(c, "User level updated successfully", response)
}

// @Summary Get buyer leaderboard
// @Description Get weekly buyer leaderboard based on spending
// @Tags gamification
// @Security BearerAuth
// @Param limit query int false "Number of entries to return" default(10)
// @Success 200 {object} utils.Response{data=LeaderboardResponse}
// @Router /gamify/leaderboard/buyers [get]
func (h *GamificationHandler) GetBuyerLeaderboard(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 10)
	if limit > 50 {
		limit = 50
	}

	entries, err := redis.GetLeaderboard("weekly_buyers", limit)
	if err != nil {
		// Fallback to database if Redis fails
		entries = h.generateBuyerLeaderboardFromDB(limit)
	}

	response := LeaderboardResponse{
		Type:    "buyers",
		Period:  "weekly",
		Entries: entries,
	}

	return utils.SuccessResponse(c, "Buyer leaderboard retrieved successfully", response)
}

// @Summary Get seller leaderboard
// @Description Get monthly seller leaderboard based on sales
// @Tags gamification
// @Security BearerAuth
// @Param limit query int false "Number of entries to return" default(10)
// @Success 200 {object} utils.Response{data=LeaderboardResponse}
// @Router /gamify/leaderboard/sellers [get]
func (h *GamificationHandler) GetSellerLeaderboard(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 10)
	if limit > 50 {
		limit = 50
	}

	entries, err := redis.GetLeaderboard("monthly_sellers", limit)
	if err != nil {
		// Fallback to database if Redis fails
		entries = h.generateSellerLeaderboardFromDB(limit)
	}

	response := LeaderboardResponse{
		Type:    "sellers",
		Period:  "monthly",
		Entries: entries,
	}

	return utils.SuccessResponse(c, "Seller leaderboard retrieved successfully", response)
}

// Helper functions

func (h *GamificationHandler) calculateLevel(xp int) models.UserLevel {
	if xp >= 5000 {
		return models.LevelPlatinum
	} else if xp >= 1500 {
		return models.LevelGold
	} else if xp >= 500 {
		return models.LevelSilver
	}
	return models.LevelBronze
}

func (h *GamificationHandler) calculateXPToNextLevel(currentXP int, currentLevel models.UserLevel) int {
	switch currentLevel {
	case models.LevelBronze:
		return 500 - currentXP
	case models.LevelSilver:
		return 1500 - currentXP
	case models.LevelGold:
		return 5000 - currentXP
	case models.LevelPlatinum:
		return 0 // Max level
	}
	return 0
}

func (h *GamificationHandler) calculateLevelProgress(currentXP int, currentLevel models.UserLevel) float64 {
	switch currentLevel {
	case models.LevelBronze:
		return float64(currentXP) / 500.0 * 100
	case models.LevelSilver:
		return float64(currentXP-500) / 1000.0 * 100
	case models.LevelGold:
		return float64(currentXP-1500) / 3500.0 * 100
	case models.LevelPlatinum:
		return 100.0
	}
	return 0.0
}

func (h *GamificationHandler) getLevelInfo(level models.UserLevel) map[string]interface{} {
	levelInfo := map[string]interface{}{
		"name": level,
	}

	switch level {
	case models.LevelBronze:
		levelInfo["min_xp"] = 0
		levelInfo["max_xp"] = 499
		levelInfo["color"] = "#CD7F32"
	case models.LevelSilver:
		levelInfo["min_xp"] = 500
		levelInfo["max_xp"] = 1499
		levelInfo["color"] = "#C0C0C0"
	case models.LevelGold:
		levelInfo["min_xp"] = 1500
		levelInfo["max_xp"] = 4999
		levelInfo["color"] = "#FFD700"
	case models.LevelPlatinum:
		levelInfo["min_xp"] = 5000
		levelInfo["max_xp"] = nil
		levelInfo["color"] = "#E5E4E2"
	}

	return levelInfo
}

func (h *GamificationHandler) checkAndAwardBadges(user *models.User) []models.UserBadge {
	var newBadges []models.UserBadge

	// Get all badges
	var badges []models.Badge
	database.DB.Find(&badges)

	// Get user's existing badges
	var existingBadges []models.UserBadge
	database.DB.Where("user_id = ?", user.ID).Find(&existingBadges)

	existingBadgeTypes := make(map[models.BadgeType]bool)
	for _, badge := range existingBadges {
		var badgeInfo models.Badge
		database.DB.First(&badgeInfo, badge.BadgeID)
		existingBadgeTypes[badgeInfo.Type] = true
	}

	// Check each badge type
	for _, badge := range badges {
		if existingBadgeTypes[badge.Type] {
			continue // User already has this badge
		}

		shouldAward := false

		switch badge.Type {
		case models.BadgeFirstOrder:
			var orderCount int64
			database.DB.Model(&models.Order{}).Where("buyer_id = ?", user.ID).Count(&orderCount)
			shouldAward = orderCount >= 1

		case models.BadgeTopSeller:
			var salesCount int64
			database.DB.Model(&models.Order{}).
				Joins("JOIN order_items ON orders.id = order_items.order_id").
				Joins("JOIN products ON order_items.product_id = products.id").
				Where("products.seller_id = ? AND orders.status = ?", user.ID, models.OrderDelivered).
				Count(&salesCount)
			shouldAward = salesCount >= 10

		case models.BadgeBigSpender:
			shouldAward = user.TotalSpent >= 5000

		case models.BadgeEarlyBird:
			var userCount int64
			database.DB.Model(&models.User{}).Count(&userCount)
			shouldAward = userCount <= 100

		case models.BadgeReviewer:
			// This would require a reviews table - placeholder for now
			shouldAward = false

		case models.BadgeReferrer:
			// This would require a referrals table - placeholder for now
			shouldAward = false
		}

		if shouldAward {
			userBadge := models.UserBadge{
				BaseModel: models.BaseModel{ID: uuid.New()},
				UserID:    user.ID,
				BadgeID:   badge.ID,
				EarnedAt:  time.Now(),
			}

			if err := database.DB.Create(&userBadge).Error; err == nil {
				// Award XP for badge
				if badge.XPReward > 0 {
					h.awardXP(user.ID, badge.XPReward, fmt.Sprintf("Badge: %s", badge.Name))
				}
				newBadges = append(newBadges, userBadge)
			}
		}
	}

	return newBadges
}

func (h *GamificationHandler) awardXP(userID uuid.UUID, amount int, reason string) {
	xpTransaction := models.XPTransaction{
		BaseModel: models.BaseModel{ID: uuid.New()},
		UserID:    userID,
		Amount:    amount,
		Reason:    reason,
	}
	database.DB.Create(&xpTransaction)
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("total_xp", database.DB.Raw("total_xp + ?", amount))
}

func (h *GamificationHandler) updateLeaderboards(user *models.User, newXP int) {
	userData := map[string]interface{}{
		"name":        user.Name,
		"level":       user.Level,
		"badge_count": 0, // This would be calculated
	}

	if user.Role == models.RoleBuyer {
		redis.SetLeaderboardEntry("weekly_buyers", user.ID.String(), user.TotalSpent, userData)
	} else if user.Role == models.RoleSeller {
		redis.SetLeaderboardEntry("monthly_sellers", user.ID.String(), user.TotalSales, userData)
	}
}

func (h *GamificationHandler) generateBuyerLeaderboardFromDB(limit int) []models.LeaderboardEntry {
	var users []models.User
	database.DB.Where("role = ?", models.RoleBuyer).
		Order("total_spent DESC").
		Limit(limit).
		Find(&users)

	entries := make([]models.LeaderboardEntry, len(users))
	for i, user := range users {
		entries[i] = models.LeaderboardEntry{
			UserID: user.ID,
			Name:   user.Name,
			Score:  user.TotalSpent,
			Rank:   i + 1,
			Level:  user.Level,
		}
	}

	return entries
}

func (h *GamificationHandler) generateSellerLeaderboardFromDB(limit int) []models.LeaderboardEntry {
	var users []models.User
	database.DB.Where("role = ?", models.RoleSeller).
		Order("total_sales DESC").
		Limit(limit).
		Find(&users)

	entries := make([]models.LeaderboardEntry, len(users))
	for i, user := range users {
		entries[i] = models.LeaderboardEntry{
			UserID: user.ID,
			Name:   user.Name,
			Score:  user.TotalSales,
			Rank:   i + 1,
			Level:  user.Level,
		}
	}

	return entries
}
