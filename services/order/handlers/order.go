package handlers

import (
	"fmt"
	"math/rand"
	"time"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OrderHandler struct {
	config *config.Config
}

type CreateOrderRequest struct {
	Items           []OrderItemRequest `json:"items" validate:"required"`
	ShippingAddress string             `json:"shipping_address" validate:"required"`
	Notes           string             `json:"notes"`
}

type OrderItemRequest struct {
	ProductID uuid.UUID `json:"product_id" validate:"required"`
	Quantity  int       `json:"quantity" validate:"required,min=1"`
}

type UpdateOrderStatusRequest struct {
	Status models.OrderStatus `json:"status" validate:"required"`
	Notes  string             `json:"notes"`
}

type OrderListResponse struct {
	Orders []models.Order `json:"orders"`
	Total  int64          `json:"total"`
	Page   int            `json:"page"`
	Limit  int            `json:"limit"`
}

func NewOrderHandler(cfg *config.Config) *OrderHandler {
	return &OrderHandler{
		config: cfg,
	}
}

// @Summary Create new order
// @Description Create a new order from cart items
// @Tags orders
// @Security BearerAuth
// @Param request body CreateOrderRequest true "Create order request"
// @Success 201 {object} utils.Response{data=models.Order}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /orders [post]
func (h *OrderHandler) CreateOrder(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	var req CreateOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	if len(req.Items) == 0 {
		return utils.ValidationErrorResponse(c, "Order must contain at least one item")
	}

	if req.ShippingAddress == "" {
		return utils.ValidationErrorResponse(c, "Shipping address is required")
	}

	// Start transaction
	tx := database.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Generate order number
	orderNumber := h.generateOrderNumber()

	// Create order
	order := models.Order{
		BaseModel:       models.BaseModel{ID: uuid.New()},
		OrderNumber:     orderNumber,
		BuyerID:         userID,
		Status:          models.OrderPending,
		ShippingAddress: req.ShippingAddress,
		Notes:           req.Notes,
	}

	var totalAmount float64
	var orderItems []models.OrderItem

	// Process each item
	for _, item := range req.Items {
		// Get product
		var product models.Product
		if err := tx.First(&product, item.ProductID).Error; err != nil {
			tx.Rollback()
			return utils.NotFoundResponse(c, fmt.Sprintf("Product %s not found", item.ProductID))
		}

		// Check if product is active
		if !product.IsActive {
			tx.Rollback()
			return utils.ValidationErrorResponse(c, fmt.Sprintf("Product %s is not available", product.Name))
		}

		// Check stock
		if product.Stock < item.Quantity {
			tx.Rollback()
			return utils.ValidationErrorResponse(c, fmt.Sprintf("Insufficient stock for product %s. Available: %d, Requested: %d", product.Name, product.Stock, item.Quantity))
		}

		// Calculate item total
		itemTotal := product.Price * float64(item.Quantity)
		totalAmount += itemTotal

		// Create order item
		orderItem := models.OrderItem{
			BaseModel: models.BaseModel{ID: uuid.New()},
			OrderID:   order.ID,
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     product.Price, // Store price at time of order
		}

		orderItems = append(orderItems, orderItem)

		// Update product stock
		if err := tx.Model(&product).Update("stock", product.Stock-item.Quantity).Error; err != nil {
			tx.Rollback()
			return utils.InternalServerErrorResponse(c, "Failed to update product stock", err)
		}
	}

	order.TotalAmount = totalAmount

	// Save order
	if err := tx.Create(&order).Error; err != nil {
		tx.Rollback()
		return utils.InternalServerErrorResponse(c, "Failed to create order", err)
	}

	// Save order items
	for _, item := range orderItems {
		if err := tx.Create(&item).Error; err != nil {
			tx.Rollback()
			return utils.InternalServerErrorResponse(c, "Failed to create order items", err)
		}
	}

	// Update user's total spent
	if err := tx.Model(&models.User{}).Where("id = ?", userID).Update("total_spent", gorm.Expr("total_spent + ?", totalAmount)).Error; err != nil {
		tx.Rollback()
		return utils.InternalServerErrorResponse(c, "Failed to update user stats", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to commit transaction", err)
	}

	// Load order with relationships
	database.DB.Preload("Buyer").Preload("Items.Product").First(&order, order.ID)

	// Award XP for first order (async)
	go h.awardFirstOrderXP(userID)

	return c.Status(fiber.StatusCreated).JSON(utils.Response{
		Success: true,
		Message: "Order created successfully",
		Data:    order,
	})
}

// @Summary Get order by ID
// @Description Get detailed information about a specific order
// @Tags orders
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Success 200 {object} utils.Response{data=models.Order}
// @Failure 404 {object} utils.Response
// @Router /orders/{id} [get]
func (h *OrderHandler) GetOrder(c *fiber.Ctx) error {
	orderIDParam := c.Params("id")
	orderID, err := uuid.Parse(orderIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid order ID")
	}

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	// Get order with relationships
	var order models.Order
	query := database.DB.Preload("Buyer").Preload("Items.Product.Seller").Preload("Payment")

	// Users can only see their own orders (buyers see orders they placed, sellers see orders for their products)
	userRole, _ := c.Locals("user_role").(models.UserRole)
	if userRole == models.RoleBuyer {
		query = query.Where("buyer_id = ?", userID)
	} else if userRole == models.RoleSeller {
		query = query.Joins("JOIN order_items ON orders.id = order_items.order_id").
			Joins("JOIN products ON order_items.product_id = products.id").
			Where("products.seller_id = ?", userID)
	}

	if err := query.First(&order, orderID).Error; err != nil {
		return utils.NotFoundResponse(c, "Order not found")
	}

	return utils.SuccessResponse(c, "Order retrieved successfully", order)
}

// @Summary Get user orders
// @Description Get paginated list of orders for a user
// @Tags orders
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param status query string false "Filter by status"
// @Success 200 {object} utils.Response{data=OrderListResponse}
// @Failure 404 {object} utils.Response
// @Router /users/{id}/orders [get]
func (h *OrderHandler) GetUserOrders(c *fiber.Ctx) error {
	userIDParam := c.Params("id")
	targetUserID, err := uuid.Parse(userIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid user ID")
	}

	currentUserID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	// Users can only see their own orders
	if currentUserID != targetUserID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only view your own orders", nil)
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if limit > 100 {
		limit = 100
	}

	offset := (page - 1) * limit

	// Build query
	query := database.DB.Model(&models.Order{}).Where("buyer_id = ?", targetUserID)

	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get orders
	var orders []models.Order
	if err := query.Preload("Items.Product").Preload("Payment").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&orders).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to get orders", err)
	}

	response := OrderListResponse{
		Orders: orders,
		Total:  total,
		Page:   page,
		Limit:  limit,
	}

	return utils.SuccessResponse(c, "Orders retrieved successfully", response)
}

// @Summary Update order status
// @Description Update the status of an order (seller only for their products)
// @Tags orders
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Param request body UpdateOrderStatusRequest true "Update status request"
// @Success 200 {object} utils.Response{data=models.Order}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /orders/{id}/status [put]
func (h *OrderHandler) UpdateOrderStatus(c *fiber.Ctx) error {
	orderIDParam := c.Params("id")
	orderID, err := uuid.Parse(orderIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid order ID")
	}

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	var req UpdateOrderStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Validate status
	validStatuses := []models.OrderStatus{
		models.OrderPending, models.OrderConfirmed, models.OrderProcessing,
		models.OrderShipped, models.OrderDelivered, models.OrderCancelled,
	}
	
	isValidStatus := false
	for _, status := range validStatuses {
		if req.Status == status {
			isValidStatus = true
			break
		}
	}
	
	if !isValidStatus {
		return utils.ValidationErrorResponse(c, "Invalid order status")
	}

	// Get order and check permissions
	var order models.Order
	if err := database.DB.Preload("Items.Product").First(&order, orderID).Error; err != nil {
		return utils.NotFoundResponse(c, "Order not found")
	}

	// Check if user is seller of any product in this order
	userRole, _ := c.Locals("user_role").(models.UserRole)
	if userRole == models.RoleSeller {
		hasPermission := false
		for _, item := range order.Items {
			if item.Product.SellerID == userID {
				hasPermission = true
				break
			}
		}
		if !hasPermission {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only update orders for your products", nil)
		}
	} else {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only sellers can update order status", nil)
	}

	// Update order status
	order.Status = req.Status
	if req.Notes != "" {
		order.Notes = req.Notes
	}

	if err := database.DB.Save(&order).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to update order status", err)
	}

	// Award XP and update seller stats if order is delivered
	if req.Status == models.OrderDelivered {
		go h.processDeliveredOrder(&order)
	}

	// Load updated order with relationships
	database.DB.Preload("Buyer").Preload("Items.Product.Seller").Preload("Payment").First(&order, order.ID)

	return utils.SuccessResponse(c, "Order status updated successfully", order)
}

// Helper functions

func (h *OrderHandler) generateOrderNumber() string {
	// Generate order number: ORD-YYYYMMDD-XXXXXX
	now := time.Now()
	dateStr := now.Format("20060102")
	randomNum := rand.Intn(999999)
	return fmt.Sprintf("ORD-%s-%06d", dateStr, randomNum)
}

func (h *OrderHandler) awardFirstOrderXP(userID uuid.UUID) {
	// Check if this is user's first order
	var orderCount int64
	database.DB.Model(&models.Order{}).Where("buyer_id = ?", userID).Count(&orderCount)

	if orderCount == 1 {
		// Award first order badge and XP
		h.callGamificationService(userID, 50, "First Order", "")
		h.checkAndAwardBadge(userID, models.BadgeFirstOrder)
	}
}

func (h *OrderHandler) processDeliveredOrder(order *models.Order) {
	// Update seller stats and award XP
	for _, item := range order.Items {
		sellerID := item.Product.SellerID
		saleAmount := item.Price * float64(item.Quantity)

		// Update seller's total sales
		database.DB.Model(&models.User{}).Where("id = ?", sellerID).
			Update("total_sales", gorm.Expr("total_sales + ?", saleAmount))

		// Award XP to seller (10 XP per ₵100 in sales)
		xpAmount := int(saleAmount / 100 * 10)
		if xpAmount > 0 {
			h.callGamificationService(sellerID, xpAmount, "Product Sale", order.ID.String())
		}
	}

	// Award XP to buyer (5 XP per ₵100 spent)
	buyerXP := int(order.TotalAmount / 100 * 5)
	if buyerXP > 0 {
		h.callGamificationService(order.BuyerID, buyerXP, "Order Completed", order.ID.String())
	}

	// Check for badges
	h.checkAndAwardBadge(order.BuyerID, models.BadgeBigSpender)
	
	// Check top seller badge for all sellers in this order
	for _, item := range order.Items {
		h.checkAndAwardBadge(item.Product.SellerID, models.BadgeTopSeller)
	}
}

func (h *OrderHandler) callGamificationService(userID uuid.UUID, xpAmount int, reason, reference string) {
	// In a real microservices setup, this would be an HTTP call to the gamification service
	// For now, we'll directly create the XP transaction
	xpTransaction := models.XPTransaction{
		BaseModel: models.BaseModel{ID: uuid.New()},
		UserID:    userID,
		Amount:    xpAmount,
		Reason:    reason,
		Reference: reference,
	}
	database.DB.Create(&xpTransaction)
	database.DB.Model(&models.User{}).Where("id = ?", userID).Update("total_xp", gorm.Expr("total_xp + ?", xpAmount))
}

func (h *OrderHandler) checkAndAwardBadge(userID uuid.UUID, badgeType models.BadgeType) {
	// Get user
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		return
	}

	// Check if user already has this badge
	var existingBadge models.UserBadge
	if err := database.DB.Joins("JOIN badges ON user_badges.badge_id = badges.id").
		Where("user_badges.user_id = ? AND badges.type = ?", userID, badgeType).
		First(&existingBadge).Error; err == nil {
		return // User already has this badge
	}

	// Get badge
	var badge models.Badge
	if err := database.DB.Where("type = ?", badgeType).First(&badge).Error; err != nil {
		return
	}

	shouldAward := false

	switch badgeType {
	case models.BadgeFirstOrder:
		var orderCount int64
		database.DB.Model(&models.Order{}).Where("buyer_id = ?", userID).Count(&orderCount)
		shouldAward = orderCount >= 1

	case models.BadgeTopSeller:
		var salesCount int64
		database.DB.Model(&models.Order{}).
			Joins("JOIN order_items ON orders.id = order_items.order_id").
			Joins("JOIN products ON order_items.product_id = products.id").
			Where("products.seller_id = ? AND orders.status = ?", userID, models.OrderDelivered).
			Count(&salesCount)
		shouldAward = salesCount >= 10

	case models.BadgeBigSpender:
		shouldAward = user.TotalSpent >= 5000
	}

	if shouldAward {
		userBadge := models.UserBadge{
			BaseModel: models.BaseModel{ID: uuid.New()},
			UserID:    userID,
			BadgeID:   badge.ID,
			EarnedAt:  time.Now(),
		}
		database.DB.Create(&userBadge)

		// Award XP for badge
		if badge.XPReward > 0 {
			h.callGamificationService(userID, badge.XPReward, fmt.Sprintf("Badge: %s", badge.Name), "")
		}
	}
}
