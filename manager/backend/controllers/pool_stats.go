package controllers

import (
	"net/http"
	"time"
	"xiaozhi/manager/backend/storage"

	"github.com/gin-gonic/gin"
)

// PoolStatsController 资源池统计控制器
type PoolStatsController struct {
	storage *storage.PoolStatsStorage
}

// NewPoolStatsController 创建资源池统计控制器
func NewPoolStatsController() *PoolStatsController {
	return &PoolStatsController{
		storage: storage.GetPoolStatsStorage(),
	}
}

// ReportPoolStats 接收主服务上报的统计数据（内部接口，无需认证）
func (c *PoolStatsController) ReportPoolStats(ctx *gin.Context) {
	var request struct {
		Stats map[string]interface{} `json:"stats" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 保存统计数据
	c.storage.AddStats(request.Stats)

	ctx.JSON(http.StatusOK, gin.H{
		"message":   "统计数据上报成功",
		"timestamp": time.Now().Unix(),
	})
}

// GetPoolStats 获取资源池统计数据（管理员接口）
func (c *PoolStatsController) GetPoolStats(ctx *gin.Context) {
	// 获取查询参数
	queryType := ctx.DefaultQuery("type", "latest") // latest, all, range

	switch queryType {
	case "latest":
		// 获取最新数据
		latest := c.storage.GetLatestStats()
		if latest == nil {
			ctx.JSON(http.StatusOK, gin.H{
				"data":    nil,
				"message": "暂无统计数据",
			})
			return
		}
		ctx.JSON(http.StatusOK, gin.H{
			"data": latest,
		})

	case "all":
		// 获取所有数据（最近24小时）
		allStats := c.storage.GetAllStats()
		ctx.JSON(http.StatusOK, gin.H{
			"data":  allStats,
			"count": len(allStats),
		})

	case "range":
		// 根据时间范围获取数据
		startStr := ctx.Query("start")
		endStr := ctx.Query("end")

		if startStr == "" || endStr == "" {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "时间范围参数 start 和 end 不能为空"})
			return
		}

		start, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "开始时间格式错误，请使用 RFC3339 格式"})
			return
		}

		end, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "结束时间格式错误，请使用 RFC3339 格式"})
			return
		}

		stats := c.storage.GetStatsByTimeRange(start, end)
		ctx.JSON(http.StatusOK, gin.H{
			"data":  stats,
			"count": len(stats),
		})

	default:
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询类型，支持: latest, all, range"})
	}
}

// GetPoolStatsSummary 获取统计摘要信息
func (c *PoolStatsController) GetPoolStatsSummary(ctx *gin.Context) {
	latest := c.storage.GetLatestStats()

	summary := gin.H{
		"total_records":    0,
		"storage_duration": "仅保存最新数据",
		"oldest_timestamp": nil,
		"newest_timestamp": nil,
	}

	if latest != nil {
		summary["total_records"] = 1
		summary["newest_timestamp"] = latest.Timestamp
		summary["oldest_timestamp"] = latest.Timestamp
	}

	ctx.JSON(http.StatusOK, gin.H{
		"data": summary,
	})
}
