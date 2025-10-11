package pixiv

import (
	"github.com/jinzhu/gorm"
	"gorm.io/datatypes"
)

// IllustCache 插画缓存表
type IllustCache struct {
	gorm.Model

	PID         int64          `gorm:"unique_index:idx_keyword_pid;not null;column:pid"` // Pixiv 作品 ID
	UID         int64          `gorm:"default:0;not null;column:uid"`                    // 插画作者的id
	Keyword     string         `gorm:"unique_index:idx_keyword_pid;type:varchar(255)"`   // 搜索关键词
	Title       string         `gorm:"type:varchar(255)"`                                // 标题
	AuthorName  string         `gorm:"type:varchar(255)"`                                // 用户名
	ImageURL    string         `gorm:"type:varchar(500)"`                                // 大图地址
	OriginalURL string         `gorm:"type:varchar(500)"`                                // 原图地址
	R18         bool           `gorm:"not null;default:false"`                           // 是否为 R-18 作品
	Bookmarks   int64          // 收藏数
	TotalView   int64          // 总浏览数
	CreateDate  string         // 创建日期
	PageCount   int64          // 页数
	Tags        datatypes.JSON `gorm:"type:json"` // 插画的所有标签 方便后续查找
}

// SentImage 已发送记录表
type SentImage struct {
	gorm.Model

	GroupID int64 `gorm:"index:idx_group_pid;not null"`            // 群组 ID
	PID     int64 `gorm:"index:idx_group_pid;not null;column:pid"` // 插画 PID
}

type RefreshToken struct {
	gorm.Model

	User int64 `gorm:"unique"`

	Token string
}

var (
	tokenResp *TokenStore
	db        = &gorm.DB{}
)
