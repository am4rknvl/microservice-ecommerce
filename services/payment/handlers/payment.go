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
	"gorm.io/gorm"
)

type PaymentHandler struct {
	config *config.Config
}

type InitiatePaymentRequest struct {
	OrderID uuid.UUID             `json:"order_id" validate:"required"`
	Method  models.PaymentMethod  `json:"method" validate:"required"`
	Phone   string                `json:"phone"` // Required for mobile payments
}

type PaymentStatusResponse struct {
	*models.Payment
	Order *models.Order `json:"order,omitempty"`
}

type MockPaymentResponse struct {
	TransactionID string `json:"transaction_id"`
	Reference     string `json:"reference"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	RedirectURL   string `json:"redirect_url,omitempty"`
}

func NewPaymentHandler(cfg *config.Config) *PaymentHandler {
	return &PaymentHandler{
		config: cfg,
	}
}

// @Summary Initiate payment
// @Description Initiate payment for an order using Telebirr, CBE Birr, or Cash
// @Tags payments
// @Security BearerAuth
// @Param request body InitiatePaymentRequest true "Initiate payment request"
// @Success 200 {object} utils.Response{data=MockPaymentResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /payments/initiate [post]
func (h *PaymentHandler) InitiatePayment(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	var req InitiatePaymentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Validate payment method
	validMethods := []models.PaymentMethod{
		models.PaymentTelebirr, models.PaymentCBEBirr, models.PaymentCash,
	}
	
	isValidMethod := false
	for _, method := range validMethods {
		if req.Method == method {
			isValidMethod = true
			break
		}
	}
	
	if !isValidMethod {
		return utils.ValidationErrorResponse(c, "Invalid payment method")
	}

	// Validate phone for mobile payments
	if (req.Method == models.PaymentTelebirr || req.Method == models.PaymentCBEBirr) && req.Phone == "" {
		return utils.ValidationErrorResponse(c, "Phone number is required for mobile payments")
	}

	// Get order
	var order models.Order
	if err := database.DB.Preload("Items.Product").First(&order, req.OrderID).Error; err != nil {
		return utils.NotFoundResponse(c, "Order not found")
	}

	// Check if user owns this order
	if order.BuyerID != userID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only pay for your own orders", nil)
	}

	// Check if order is in pending status
	if order.Status != models.OrderPending {
		return utils.ValidationErrorResponse(c, "Order is not in pending status")
	}

	// Check if payment already exists
	var existingPayment models.Payment
	if err := database.DB.Where("order_id = ?", req.OrderID).First(&existingPayment).Error; err == nil {
		if existingPayment.Status == models.PaymentCompleted {
			return utils.ValidationErrorResponse(c, "Order has already been paid")
		}
		if existingPayment.Status == models.PaymentPending {
			return utils.ValidationErrorResponse(c, "Payment is already in progress")
		}
	}

	// Create payment record
	payment := models.Payment{
		BaseModel: models.BaseModel{ID: uuid.New()},
		OrderID:   req.OrderID,
		Amount:    order.TotalAmount,
		Method:    req.Method,
		Status:    models.PaymentPending,
	}

	if err := database.DB.Create(&payment).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create payment record", err)
	}

	// Process payment based on method
	var response MockPaymentResponse
	var err error

	switch req.Method {
	case models.PaymentTelebirr:
		response, err = h.processTelebirrPayment(&payment, req.Phone)
	case models.PaymentCBEBirr:
		response, err = h.processCBEBirrPayment(&payment, req.Phone)
	case models.PaymentCash:
		response, err = h.processCashPayment(&payment)
	}

	if err != nil {
		// Update payment status to failed
		database.DB.Model(&payment).Updates(map[string]interface{}{
			"status": models.PaymentFailed,
		})
		return utils.InternalServerErrorResponse(c, "Payment processing failed", err)
	}

	// Update payment with transaction details
	database.DB.Model(&payment).Updates(map[string]interface{}{
		"transaction_id": response.TransactionID,
		"reference":      response.Reference,
	})

	// Store payment session in Redis for status checking
	paymentSession := map[string]interface{}{
		"payment_id":     payment.ID.String(),
		"order_id":       order.ID.String(),
		"user_id":        userID.String(),
		"amount":         payment.Amount,
		"method":         payment.Method,
		"transaction_id": response.TransactionID,
		"created_at":     time.Now(),
	}
	
	sessionKey := fmt.Sprintf("payment_session:%s", response.TransactionID)
	redis.Set(sessionKey, paymentSession, 30*60) // 30 minutes

	return utils.SuccessResponse(c, "Payment initiated successfully", response)
}

// @Summary Get payment status
// @Description Check the status of a payment transaction
// @Tags payments
// @Security BearerAuth
// @Param id path string true "Payment ID or Transaction ID"
// @Success 200 {object} utils.Response{data=PaymentStatusResponse}
// @Failure 404 {object} utils.Response
// @Router /payments/status/{id} [get]
func (h *PaymentHandler) GetPaymentStatus(c *fiber.Ctx) error {
	paymentID := c.Params("id")

	var payment models.Payment
	var err error

	// Try to find by payment ID first
	if uuid, parseErr := uuid.Parse(paymentID); parseErr == nil {
		err = database.DB.Preload("Order").First(&payment, uuid).Error
	} else {
		// Try to find by transaction ID
		err = database.DB.Preload("Order").Where("transaction_id = ?", paymentID).First(&payment).Error
	}

	if err != nil {
		return utils.NotFoundResponse(c, "Payment not found")
	}

	// For pending payments, simulate status check with payment provider
	if payment.Status == models.PaymentPending {
		// Simulate random payment completion (70% success rate)
		if rand.Float32() < 0.7 {
			h.completePayment(&payment)
		} else if time.Since(payment.CreatedAt) > 15*time.Minute {
			// Auto-fail payments older than 15 minutes
			h.failPayment(&payment, "Payment timeout")
		}
	}

	response := PaymentStatusResponse{
		Payment: &payment,
		Order:   &payment.Order,
	}

	return utils.SuccessResponse(c, "Payment status retrieved successfully", response)
}

// @Summary Get payment methods
// @Description Get available payment methods with their details
// @Tags payments
// @Success 200 {object} utils.Response{data=[]map[string]interface{}}
// @Router /payments/methods [get]
func (h *PaymentHandler) GetPaymentMethods(c *fiber.Ctx) error {
	methods := []map[string]interface{}{
		{
			"method":      models.PaymentTelebirr,
			"name":        "Telebirr",
			"description": "Pay using Telebirr mobile wallet",
			"icon":        "telebirr-icon.png",
			"requires_phone": true,
			"processing_fee": 0.02, // 2%
		},
		{
			"method":      models.PaymentCBEBirr,
			"name":        "CBE Birr",
			"description": "Pay using Commercial Bank of Ethiopia mobile banking",
			"icon":        "cbe-icon.png",
			"requires_phone": true,
			"processing_fee": 0.015, // 1.5%
		},
		{
			"method":      models.PaymentCash,
			"name":        "Cash on Delivery",
			"description": "Pay with cash when your order is delivered",
			"icon":        "cash-icon.png",
			"requires_phone": false,
			"processing_fee": 0.0, // No fee
		},
	}

	return utils.SuccessResponse(c, "Payment methods retrieved successfully", methods)
}

// Mock payment processing functions

func (h *PaymentHandler) processTelebirrPayment(payment *models.Payment, phone string) (MockPaymentResponse, error) {
	// Simulate Telebirr API integration
	transactionID := h.generateTransactionID("TB")
	reference := h.generateReference()

	// In a real implementation, you would:
	// 1. Call Telebirr API to initiate payment
	// 2. Handle webhook responses
	// 3. Verify payment status

	response := MockPaymentResponse{
		TransactionID: transactionID,
		Reference:     reference,
		Status:        "pending",
		Message:       fmt.Sprintf("Payment initiated. Please complete the transaction on your Telebirr app using phone %s", phone),
		RedirectURL:   fmt.Sprintf("telebirr://pay?ref=%s&amount=%.2f", reference, payment.Amount),
	}

	// Simulate async payment completion (in real scenario, this would be a webhook)
	go h.simulateAsyncPaymentCompletion(payment, 10*time.Second)

	return response, nil
}

func (h *PaymentHandler) processCBEBirrPayment(payment *models.Payment, phone string) (MockPaymentResponse, error) {
	// Simulate CBE Birr API integration
	transactionID := h.generateTransactionID("CBE")
	reference := h.generateReference()

	response := MockPaymentResponse{
		TransactionID: transactionID,
		Reference:     reference,
		Status:        "pending",
		Message:       fmt.Sprintf("Payment initiated. Please complete the transaction using CBE Birr with phone %s", phone),
		RedirectURL:   fmt.Sprintf("cbebirr://pay?ref=%s&amount=%.2f", reference, payment.Amount),
	}

	// Simulate async payment completion
	go h.simulateAsyncPaymentCompletion(payment, 15*time.Second)

	return response, nil
}

func (h *PaymentHandler) processCashPayment(payment *models.Payment) (MockPaymentResponse, error) {
	// Cash payments are immediately "completed" but order remains pending until delivery
	transactionID := h.generateTransactionID("CASH")
	reference := h.generateReference()

	// Update payment status to completed for cash payments
	database.DB.Model(payment).Updates(map[string]interface{}{
		"status":         models.PaymentCompleted,
		"transaction_id": transactionID,
		"reference":      reference,
	})

	// Update order status to confirmed
	database.DB.Model(&models.Order{}).Where("id = ?", payment.OrderID).Update("status", models.OrderConfirmed)

	response := MockPaymentResponse{
		TransactionID: transactionID,
		Reference:     reference,
		Status:        "completed",
		Message:       "Cash on delivery payment confirmed. Your order will be processed.",
	}

	return response, nil
}

// Helper functions

func (h *PaymentHandler) generateTransactionID(prefix string) string {
	timestamp := time.Now().Unix()
	random := rand.Intn(999999)
	return fmt.Sprintf("%s%d%06d", prefix, timestamp, random)
}

func (h *PaymentHandler) generateReference() string {
	return fmt.Sprintf("REF%d%04d", time.Now().Unix(), rand.Intn(9999))
}

func (h *PaymentHandler) simulateAsyncPaymentCompletion(payment *models.Payment, delay time.Duration) {
	time.Sleep(delay)
	
	// 85% success rate for mobile payments
	if rand.Float32() < 0.85 {
		h.completePayment(payment)
	} else {
		h.failPayment(payment, "Payment declined by provider")
	}
}

func (h *PaymentHandler) completePayment(payment *models.Payment) {
	// Update payment status
	database.DB.Model(payment).Update("status", models.PaymentCompleted)

	// Update order status
	database.DB.Model(&models.Order{}).Where("id = ?", payment.OrderID).Update("status", models.OrderConfirmed)

	// Clear payment session
	if payment.TransactionID != "" {
		sessionKey := fmt.Sprintf("payment_session:%s", payment.TransactionID)
		redis.Delete(sessionKey)
	}

	// Award XP for successful payment (async)
	go h.awardPaymentXP(payment)
}

func (h *PaymentHandler) failPayment(payment *models.Payment, reason string) {
	// Update payment status
	database.DB.Model(payment).Updates(map[string]interface{}{
		"status": models.PaymentFailed,
	})

	// Clear payment session
	if payment.TransactionID != "" {
		sessionKey := fmt.Sprintf("payment_session:%s", payment.TransactionID)
		redis.Delete(sessionKey)
	}
}

func (h *PaymentHandler) awardPaymentXP(payment *models.Payment) {
	// Get order to find buyer
	var order models.Order
	if err := database.DB.First(&order, payment.OrderID).Error; err != nil {
		return
	}

	// Award 10 XP for successful payment
	xpTransaction := models.XPTransaction{
		BaseModel: models.BaseModel{ID: uuid.New()},
		UserID:    order.BuyerID,
		Amount:    10,
		Reason:    "Payment Completed",
		Reference: payment.ID.String(),
	}
	database.DB.Create(&xpTransaction)
	database.DB.Model(&models.User{}).Where("id = ?", order.BuyerID).Update("total_xp", gorm.Expr("total_xp + ?", 10))
}
