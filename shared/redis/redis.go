package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"playful-marketplace/shared/config"
	"playful-marketplace/shared/models"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client
var ctx = context.Background()

func Connect(cfg *config.Config) error {
	Client = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Test connection
	_, err := Client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	fmt.Println("Redis connected successfully")
	return nil
}

// Session management
func SetSession(session *models.Session) error {
	sessionData, err := json.Marshal(session)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("session:%s", session.Token)
	duration := time.Until(session.ExpiresAt)
	
	return Client.Set(ctx, key, sessionData, duration).Err()
}

func GetSession(token string) (*models.Session, error) {
	key := fmt.Sprintf("session:%s", token)
	sessionData, err := Client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var session models.Session
	err = json.Unmarshal([]byte(sessionData), &session)
	return &session, err
}

func DeleteSession(token string) error {
	key := fmt.Sprintf("session:%s", token)
	return Client.Del(ctx, key).Err()
}

// Leaderboard management
func SetLeaderboardEntry(leaderboardType string, userID string, score float64, userData map[string]interface{}) error {
	// Add to sorted set for ranking
	err := Client.ZAdd(ctx, fmt.Sprintf("leaderboard:%s", leaderboardType), redis.Z{
		Score:  score,
		Member: userID,
	}).Err()
	if err != nil {
		return err
	}

	// Store user data
	userDataJSON, err := json.Marshal(userData)
	if err != nil {
		return err
	}

	return Client.HSet(ctx, fmt.Sprintf("leaderboard:%s:users", leaderboardType), userID, userDataJSON).Err()
}

func GetLeaderboard(leaderboardType string, limit int) ([]models.LeaderboardEntry, error) {
	// Get top users from sorted set (descending order)
	members, err := Client.ZRevRangeWithScores(ctx, fmt.Sprintf("leaderboard:%s", leaderboardType), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	var entries []models.LeaderboardEntry
	for i, member := range members {
		userID := member.Member.(string)
		
		// Get user data
		userDataJSON, err := Client.HGet(ctx, fmt.Sprintf("leaderboard:%s:users", leaderboardType), userID).Result()
		if err != nil {
			continue
		}

		var userData map[string]interface{}
		if err := json.Unmarshal([]byte(userDataJSON), &userData); err != nil {
			continue
		}

		entry := models.LeaderboardEntry{
			Rank:  i + 1,
			Score: member.Score,
		}

		// Parse user data
		if name, ok := userData["name"].(string); ok {
			entry.Name = name
		}
		if level, ok := userData["level"].(string); ok {
			entry.Level = models.UserLevel(level)
		}
		if badgeCount, ok := userData["badge_count"].(float64); ok {
			entry.BadgeCount = int(badgeCount)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// Cache management
func Set(key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Client.Set(ctx, key, data, expiration).Err()
}

func Get(key string, dest interface{}) error {
	data, err := Client.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

func Delete(key string) error {
	return Client.Del(ctx, key).Err()
}

func Exists(key string) bool {
	count, _ := Client.Exists(ctx, key).Result()
	return count > 0
}
