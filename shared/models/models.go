package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base model with common fields
type BaseModel struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// User roles
type UserRole string

const (
	RoleBuyer  UserRole = "buyer"
	RoleSeller UserRole = "seller"
)

// User levels based on XP
type UserLevel string

const (
	LevelBronze   UserLevel = "bronze"   // 0-499 XP
	LevelSilver   UserLevel = "silver"   // 500-1499 XP
	LevelGold     UserLevel = "gold"     // 1500-4999 XP
	LevelPlatinum UserLevel = "platinum" // 5000+ XP
)

// Badge types
type BadgeType string

const (
	BadgeFirstOrder  BadgeType = "first_order"
	BadgeTopSeller   BadgeType = "top_seller"    // 10+ sales
	BadgeBigSpender  BadgeType = "big_spender"   // ₵5000+ spent
	BadgeEarlyBird   BadgeType = "early_bird"    // First 100 users
	BadgeReviewer    BadgeType = "reviewer"      // 10+ reviews
	BadgeReferrer    BadgeType = "referrer"      // 5+ referrals
)

// Order status
type OrderStatus string

const (
	OrderPending    OrderStatus = "pending"
	OrderConfirmed  OrderStatus = "confirmed"
	OrderProcessing OrderStatus = "processing"
	OrderShipped    OrderStatus = "shipped"
	OrderDelivered  OrderStatus = "delivered"
	OrderCancelled  OrderStatus = "cancelled"
)

// Payment status
type PaymentStatus string

const (
	PaymentPending   PaymentStatus = "pending"
	PaymentCompleted PaymentStatus = "completed"
	PaymentFailed    PaymentStatus = "failed"
	PaymentRefunded  PaymentStatus = "refunded"
)

// Payment method
type PaymentMethod string

const (
	PaymentTelebirr PaymentMethod = "telebirr"
	PaymentCBEBirr  PaymentMethod = "cbe_birr"
	PaymentCash     PaymentMethod = "cash"
)

// User model
type User struct {
	BaseModel
	Phone       string    `json:"phone" gorm:"uniqueIndex;not null"`
	Name        string    `json:"name" gorm:"not null"`
	Email       string    `json:"email" gorm:"uniqueIndex"`
	Role        UserRole  `json:"role" gorm:"not null"`
	Level       UserLevel `json:"level" gorm:"default:'bronze'"`
	TotalXP     int       `json:"total_xp" gorm:"default:0"`
	TotalSpent  float64   `json:"total_spent" gorm:"default:0"`
	TotalSales  float64   `json:"total_sales" gorm:"default:0"`
	IsActive    bool      `json:"is_active" gorm:"default:true"`
	LastLoginAt *time.Time `json:"last_login_at"`
	
	// Relationships
	Products []Product `json:"products,omitempty" gorm:"foreignKey:SellerID"`
	Orders   []Order   `json:"orders,omitempty" gorm:"foreignKey:BuyerID"`
	Badges   []UserBadge `json:"badges,omitempty" gorm:"foreignKey:UserID"`
}

// Product model
type Product struct {
	BaseModel
	Name        string  `json:"name" gorm:"not null"`
	Description string  `json:"description"`
	Price       float64 `json:"price" gorm:"not null"`
	Stock       int     `json:"stock" gorm:"default:0"`
	Category    string  `json:"category"`
	ImageURL    string  `json:"image_url"`
	IsActive    bool    `json:"is_active" gorm:"default:true"`
	SellerID    uuid.UUID `json:"seller_id" gorm:"not null"`
	
	// Relationships
	Seller     User        `json:"seller,omitempty" gorm:"foreignKey:SellerID"`
	OrderItems []OrderItem `json:"order_items,omitempty" gorm:"foreignKey:ProductID"`
}

// Order model
type Order struct {
	BaseModel
	OrderNumber string      `json:"order_number" gorm:"uniqueIndex;not null"`
	BuyerID     uuid.UUID   `json:"buyer_id" gorm:"not null"`
	TotalAmount float64     `json:"total_amount" gorm:"not null"`
	Status      OrderStatus `json:"status" gorm:"default:'pending'"`
	ShippingAddress string  `json:"shipping_address"`
	Notes       string      `json:"notes"`
	
	// Relationships
	Buyer      User        `json:"buyer,omitempty" gorm:"foreignKey:BuyerID"`
	Items      []OrderItem `json:"items,omitempty" gorm:"foreignKey:OrderID"`
	Payment    *Payment    `json:"payment,omitempty" gorm:"foreignKey:OrderID"`
}

// OrderItem model
type OrderItem struct {
	BaseModel
	OrderID   uuid.UUID `json:"order_id" gorm:"not null"`
	ProductID uuid.UUID `json:"product_id" gorm:"not null"`
	Quantity  int       `json:"quantity" gorm:"not null"`
	Price     float64   `json:"price" gorm:"not null"` // Price at time of order
	
	// Relationships
	Order   Order   `json:"order,omitempty" gorm:"foreignKey:OrderID"`
	Product Product `json:"product,omitempty" gorm:"foreignKey:ProductID"`
}

// Payment model
type Payment struct {
	BaseModel
	OrderID       uuid.UUID     `json:"order_id" gorm:"not null"`
	Amount        float64       `json:"amount" gorm:"not null"`
	Method        PaymentMethod `json:"method" gorm:"not null"`
	Status        PaymentStatus `json:"status" gorm:"default:'pending'"`
	TransactionID string        `json:"transaction_id"`
	Reference     string        `json:"reference"`
	
	// Relationships
	Order Order `json:"order,omitempty" gorm:"foreignKey:OrderID"`
}

// Badge model
type Badge struct {
	BaseModel
	Type        BadgeType `json:"type" gorm:"uniqueIndex;not null"`
	Name        string    `json:"name" gorm:"not null"`
	Description string    `json:"description"`
	IconURL     string    `json:"icon_url"`
	XPReward    int       `json:"xp_reward" gorm:"default:0"`
	
	// Relationships
	UserBadges []UserBadge `json:"user_badges,omitempty" gorm:"foreignKey:BadgeID"`
}

// UserBadge model (many-to-many relationship)
type UserBadge struct {
	BaseModel
	UserID  uuid.UUID `json:"user_id" gorm:"not null"`
	BadgeID uuid.UUID `json:"badge_id" gorm:"not null"`
	EarnedAt time.Time `json:"earned_at" gorm:"default:CURRENT_TIMESTAMP"`
	
	// Relationships
	User  User  `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Badge Badge `json:"badge,omitempty" gorm:"foreignKey:BadgeID"`
}

// XPTransaction model for tracking XP changes
type XPTransaction struct {
	BaseModel
	UserID      uuid.UUID `json:"user_id" gorm:"not null"`
	Amount      int       `json:"amount" gorm:"not null"` // Can be positive or negative
	Reason      string    `json:"reason" gorm:"not null"`
	Reference   string    `json:"reference"` // Order ID, Review ID, etc.
	
	// Relationships
	User User `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

// Session model for Redis caching
type Session struct {
	UserID    uuid.UUID `json:"user_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Leaderboard entry
type LeaderboardEntry struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Score    float64   `json:"score"`
	Rank     int       `json:"rank"`
	Level    UserLevel `json:"level"`
	BadgeCount int     `json:"badge_count"`
}
