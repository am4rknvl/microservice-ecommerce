package database

import (
	"fmt"
	"log"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect(cfg *config.Config) error {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.Port,
		cfg.Database.SSLMode,
	)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Database connected successfully")
	return nil
}

func Migrate() error {
	err := DB.AutoMigrate(
		&models.User{},
		&models.Product{},
		&models.Order{},
		&models.OrderItem{},
		&models.Payment{},
		&models.Badge{},
		&models.UserBadge{},
		&models.XPTransaction{},
	)

	if err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Seed initial badges
	seedBadges()

	log.Println("Database migration completed successfully")
	return nil
}

func seedBadges() {
	badges := []models.Badge{
		{
			Type:        models.BadgeFirstOrder,
			Name:        "First Order",
			Description: "Placed your first order",
			XPReward:    50,
		},
		{
			Type:        models.BadgeTopSeller,
			Name:        "Top Seller",
			Description: "Made 10 successful sales",
			XPReward:    200,
		},
		{
			Type:        models.BadgeBigSpender,
			Name:        "Big Spender",
			Description: "Spent over ₵5000",
			XPReward:    300,
		},
		{
			Type:        models.BadgeEarlyBird,
			Name:        "Early Bird",
			Description: "One of the first 100 users",
			XPReward:    100,
		},
		{
			Type:        models.BadgeReviewer,
			Name:        "Reviewer",
			Description: "Left 10 product reviews",
			XPReward:    150,
		},
		{
			Type:        models.BadgeReferrer,
			Name:        "Referrer",
			Description: "Referred 5 new users",
			XPReward:    250,
		},
	}

	for _, badge := range badges {
		var existingBadge models.Badge
		if err := DB.Where("type = ?", badge.Type).First(&existingBadge).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				DB.Create(&badge)
			}
		}
	}
}
