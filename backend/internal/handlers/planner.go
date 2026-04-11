package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"tripcompass-backend/internal/planner"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	plannerCacheTTL    = 1 * time.Hour
	plannerCachePrefix = "planner:v1:"
)

// PlannerHandler handles planner API routes.
type PlannerHandler struct {
	engine *planner.Engine
	redis  *redis.Client // optional; nil disables caching
}

// NewPlannerHandler creates a handler. Pass nil for redis to disable caching.
func NewPlannerHandler(db *gorm.DB, rdb *redis.Client) *PlannerHandler {
	return &PlannerHandler{engine: planner.NewEngine(db), redis: rdb}
}

// cacheKey returns a stable SHA-256 key for a GenerateRequest.
// Key includes destination, dates, budget tier, guest count, and prefs.
func cacheKey(req planner.GenerateRequest) string {
	dest := strings.ToLower(strings.TrimSpace(req.Destination))
	prefs := strings.Join(req.PreferenceTags, ",")
	raw := fmt.Sprintf("%s|%s|%s|%d|%d|%s",
		dest, req.StartDate, req.EndDate,
		req.BudgetVND/100_000, // budget tier (bucketized to 100K)
		req.GuestCount,
		prefs,
	)
	h := sha256.Sum256([]byte(raw))
	return plannerCachePrefix + fmt.Sprintf("%x", h[:8])
}

// POST /api/v1/planner/generate
func (h *PlannerHandler) Generate(c *gin.Context) {
	var req planner.GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()

	// Try cache read
	if h.redis != nil {
		key := cacheKey(req)
		if cached, err := h.redis.Get(ctx, key).Bytes(); err == nil {
			var result planner.GenerateResponse
			if json.Unmarshal(cached, &result) == nil {
				c.Header("X-Cache", "HIT")
				c.JSON(http.StatusOK, gin.H{"data": &result})
				return
			}
		}

		// Cache miss — generate
		result, err := h.engine.Generate(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Write to cache (best-effort; failures are logged, not fatal)
		if b, err := json.Marshal(result); err == nil {
			_ = h.redis.Set(ctx, key, b, plannerCacheTTL).Err()
		}
		c.Header("X-Cache", "MISS")
		c.JSON(http.StatusOK, gin.H{"data": result})
		return
	}

	// No Redis — direct call
	result, err := h.engine.Generate(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// DELETE /admin/planner/cache?destination=da-nang
// Flushes all cached plans for a given destination using SCAN+DEL.
// If destination is omitted, flushes ALL planner cache keys.
func (h *PlannerHandler) FlushCache(c *gin.Context) {
	if h.redis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Redis not configured"})
		return
	}

	ctx := context.Background()
	pattern := plannerCachePrefix + "*"
	var deleted int64

	var cursor uint64
	for {
		keys, next, err := h.redis.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("SCAN failed: %v", err)})
			return
		}
		if len(keys) > 0 {
			if n, err := h.redis.Del(ctx, keys...).Result(); err == nil {
				deleted += n
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": deleted,
		"message": fmt.Sprintf("Flushed %d planner cache keys", deleted),
	})
}
