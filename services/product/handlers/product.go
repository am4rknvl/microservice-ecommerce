package handlers

import (
	"strings"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/database"
	"playful-marketplace/shared/models"
	"playful-marketplace/shared/redis"
	"playful-marketplace/shared/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ProductHandler struct {
	config *config.Config
}

type CreateProductRequest struct {
	Name        string  `json:"name" validate:"required"`
	Description string  `json:"description"`
	Price       float64 `json:"price" validate:"required,min=0"`
	Stock       int     `json:"stock" validate:"min=0"`
	Category    string  `json:"category"`
	ImageURL    string  `json:"image_url"`
}

type UpdateProductRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Price       *float64 `json:"price"`
	Stock       *int    `json:"stock"`
	Category    string  `json:"category"`
	ImageURL    string  `json:"image_url"`
	IsActive    *bool   `json:"is_active"`
}

type ProductListResponse struct {
	Products []models.Product `json:"products"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	Limit    int              `json:"limit"`
}

func NewProductHandler(cfg *config.Config) *ProductHandler {
	return &ProductHandler{
		config: cfg,
	}
}

// @Summary Get all products
// @Description Get paginated list of products with optional filtering
// @Tags products
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param category query string false "Filter by category"
// @Param search query string false "Search in name and description"
// @Param min_price query number false "Minimum price filter"
// @Param max_price query number false "Maximum price filter"
// @Param seller_id query string false "Filter by seller ID"
// @Success 200 {object} utils.Response{data=ProductListResponse}
// @Router /products [get]
func (h *ProductHandler) GetProducts(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	category := c.Query("category")
	search := c.Query("search")
	minPrice := c.QueryFloat("min_price", 0)
	maxPrice := c.QueryFloat("max_price", 0)
	sellerID := c.Query("seller_id")

	if page < 1 {
		page = 1
	}
	if limit > 100 {
		limit = 100 // Cap at 100 for performance
	}

	offset := (page - 1) * limit

	// Build query
	query := database.DB.Model(&models.Product{}).Where("is_active = ?", true)

	if category != "" {
		query = query.Where("category ILIKE ?", "%"+category+"%")
	}

	if search != "" {
		query = query.Where("name ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	if minPrice > 0 {
		query = query.Where("price >= ?", minPrice)
	}

	if maxPrice > 0 {
		query = query.Where("price <= ?", maxPrice)
	}

	if sellerID != "" {
		if sellerUUID, err := uuid.Parse(sellerID); err == nil {
			query = query.Where("seller_id = ?", sellerUUID)
		}
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get products with seller info
	var products []models.Product
	if err := query.Preload("Seller").Offset(offset).Limit(limit).Order("created_at DESC").Find(&products).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to get products", err)
	}

	response := ProductListResponse{
		Products: products,
		Total:    total,
		Page:     page,
		Limit:    limit,
	}

	return utils.SuccessResponse(c, "Products retrieved successfully", response)
}

// @Summary Get product by ID
// @Description Get detailed information about a specific product
// @Tags products
// @Param id path string true "Product ID"
// @Success 200 {object} utils.Response{data=models.Product}
// @Failure 404 {object} utils.Response
// @Router /products/{id} [get]
func (h *ProductHandler) GetProduct(c *fiber.Ctx) error {
	productIDParam := c.Params("id")
	productID, err := uuid.Parse(productIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid product ID")
	}

	// Try to get from cache first
	cacheKey := "product:" + productID.String()
	var product models.Product
	
	if err := redis.Get(cacheKey, &product); err != nil {
		// Not in cache, get from database
		if err := database.DB.Preload("Seller").First(&product, productID).Error; err != nil {
			return utils.NotFoundResponse(c, "Product not found")
		}

		// Cache for 5 minutes
		redis.Set(cacheKey, product, 5*60)
	}

	return utils.SuccessResponse(c, "Product retrieved successfully", product)
}

// @Summary Create new product
// @Description Create a new product (seller only)
// @Tags products
// @Security BearerAuth
// @Param request body CreateProductRequest true "Create product request"
// @Success 201 {object} utils.Response{data=models.Product}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Router /products [post]
func (h *ProductHandler) CreateProduct(c *fiber.Ctx) error {
	// Check if user is a seller
	userRole, ok := c.Locals("user_role").(models.UserRole)
	if !ok || userRole != models.RoleSeller {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only sellers can create products", nil)
	}

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	var req CreateProductRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Validate required fields
	if req.Name == "" || req.Price <= 0 {
		return utils.ValidationErrorResponse(c, "Name and price are required, price must be greater than 0")
	}

	// Create product
	product := models.Product{
		BaseModel:   models.BaseModel{ID: uuid.New()},
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price,
		Stock:       req.Stock,
		Category:    req.Category,
		ImageURL:    req.ImageURL,
		IsActive:    true,
		SellerID:    userID,
	}

	if err := database.DB.Create(&product).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to create product", err)
	}

	// Load seller information
	database.DB.Preload("Seller").First(&product, product.ID)

	return c.Status(fiber.StatusCreated).JSON(utils.Response{
		Success: true,
		Message: "Product created successfully",
		Data:    product,
	})
}

// @Summary Update product
// @Description Update product information (seller only, own products)
// @Tags products
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param request body UpdateProductRequest true "Update product request"
// @Success 200 {object} utils.Response{data=models.Product}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /products/{id} [put]
func (h *ProductHandler) UpdateProduct(c *fiber.Ctx) error {
	productIDParam := c.Params("id")
	productID, err := uuid.Parse(productIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid product ID")
	}

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	// Get product
	var product models.Product
	if err := database.DB.First(&product, productID).Error; err != nil {
		return utils.NotFoundResponse(c, "Product not found")
	}

	// Check if user owns this product
	if product.SellerID != userID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only update your own products", nil)
	}

	var req UpdateProductRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ValidationErrorResponse(c, "Invalid request body")
	}

	// Update fields
	if req.Name != "" {
		product.Name = req.Name
	}
	if req.Description != "" {
		product.Description = req.Description
	}
	if req.Price != nil && *req.Price > 0 {
		product.Price = *req.Price
	}
	if req.Stock != nil && *req.Stock >= 0 {
		product.Stock = *req.Stock
	}
	if req.Category != "" {
		product.Category = req.Category
	}
	if req.ImageURL != "" {
		product.ImageURL = req.ImageURL
	}
	if req.IsActive != nil {
		product.IsActive = *req.IsActive
	}

	// Save changes
	if err := database.DB.Save(&product).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to update product", err)
	}

	// Clear cache
	cacheKey := "product:" + productID.String()
	redis.Delete(cacheKey)

	// Load seller information
	database.DB.Preload("Seller").First(&product, product.ID)

	return utils.SuccessResponse(c, "Product updated successfully", product)
}

// @Summary Delete product
// @Description Delete/deactivate a product (seller only, own products)
// @Tags products
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 200 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /products/{id} [delete]
func (h *ProductHandler) DeleteProduct(c *fiber.Ctx) error {
	productIDParam := c.Params("id")
	productID, err := uuid.Parse(productIDParam)
	if err != nil {
		return utils.ValidationErrorResponse(c, "Invalid product ID")
	}

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.UnauthorizedResponse(c, "User ID not found")
	}

	// Get product
	var product models.Product
	if err := database.DB.First(&product, productID).Error; err != nil {
		return utils.NotFoundResponse(c, "Product not found")
	}

	// Check if user owns this product
	if product.SellerID != userID {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You can only delete your own products", nil)
	}

	// Soft delete (set is_active to false)
	if err := database.DB.Model(&product).Update("is_active", false).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to delete product", err)
	}

	// Clear cache
	cacheKey := "product:" + productID.String()
	redis.Delete(cacheKey)

	return utils.SuccessResponse(c, "Product deleted successfully", nil)
}

// @Summary Search products
// @Description Advanced product search with multiple filters
// @Tags products
// @Param q query string true "Search query"
// @Param category query string false "Category filter"
// @Param min_price query number false "Minimum price"
// @Param max_price query number false "Maximum price"
// @Param sort query string false "Sort by: price_asc, price_desc, name_asc, name_desc, newest, oldest" default("newest")
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Success 200 {object} utils.Response{data=ProductListResponse}
// @Router /products/search [get]
func (h *ProductHandler) SearchProducts(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return utils.ValidationErrorResponse(c, "Search query is required")
	}

	category := c.Query("category")
	minPrice := c.QueryFloat("min_price", 0)
	maxPrice := c.QueryFloat("max_price", 0)
	sort := c.Query("sort", "newest")
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	if page < 1 {
		page = 1
	}
	if limit > 100 {
		limit = 100
	}

	offset := (page - 1) * limit

	// Build search query
	dbQuery := database.DB.Model(&models.Product{}).Where("is_active = ?", true)

	// Text search
	searchTerms := strings.Fields(strings.ToLower(query))
	for _, term := range searchTerms {
		dbQuery = dbQuery.Where("LOWER(name) LIKE ? OR LOWER(description) LIKE ? OR LOWER(category) LIKE ?", 
			"%"+term+"%", "%"+term+"%", "%"+term+"%")
	}

	// Filters
	if category != "" {
		dbQuery = dbQuery.Where("category ILIKE ?", "%"+category+"%")
	}
	if minPrice > 0 {
		dbQuery = dbQuery.Where("price >= ?", minPrice)
	}
	if maxPrice > 0 {
		dbQuery = dbQuery.Where("price <= ?", maxPrice)
	}

	// Sorting
	var orderBy string
	switch sort {
	case "price_asc":
		orderBy = "price ASC"
	case "price_desc":
		orderBy = "price DESC"
	case "name_asc":
		orderBy = "name ASC"
	case "name_desc":
		orderBy = "name DESC"
	case "oldest":
		orderBy = "created_at ASC"
	default: // newest
		orderBy = "created_at DESC"
	}

	// Get total count
	var total int64
	dbQuery.Count(&total)

	// Get products
	var products []models.Product
	if err := dbQuery.Preload("Seller").Order(orderBy).Offset(offset).Limit(limit).Find(&products).Error; err != nil {
		return utils.InternalServerErrorResponse(c, "Failed to search products", err)
	}

	response := ProductListResponse{
		Products: products,
		Total:    total,
		Page:     page,
		Limit:    limit,
	}

	return utils.SuccessResponse(c, "Products found successfully", response)
}

// @Summary Get product categories
// @Description Get list of all product categories
// @Tags products
// @Success 200 {object} utils.Response{data=[]string}
// @Router /products/categories [get]
func (h *ProductHandler) GetCategories(c *fiber.Ctx) error {
	var categories []string
	
	// Try to get from cache first
	cacheKey := "product_categories"
	if err := redis.Get(cacheKey, &categories); err != nil {
		// Not in cache, get from database
		if err := database.DB.Model(&models.Product{}).
			Where("is_active = ? AND category != ''", true).
			Distinct("category").
			Pluck("category", &categories).Error; err != nil {
			return utils.InternalServerErrorResponse(c, "Failed to get categories", err)
		}

		// Cache for 1 hour
		redis.Set(cacheKey, categories, 3600)
	}

	return utils.SuccessResponse(c, "Categories retrieved successfully", categories)
}
