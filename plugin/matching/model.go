package matching

import (
	fcext "github.com/FloatTech/floatbox/ctxext"
	"github.com/jinzhu/gorm"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// RejectedMatchUser 用于检查是否已被用户拒绝过匹配邀请
type RejectedMatchUser struct {
	ID     uint  `gorm:"primary_key"`
	UserID int64 `gorm:"unique"`
}

type User struct {
	ID             uint              `gorm:"primary_key"`
	UserID         int64             `gorm:"unique" json:"user_id"`
	UserName       string            `json:"user_name"`
	GroupID        int64             `json:"group_id"`
	LimitTime      int64             `gorm:"default:900" json:"limit_time"`
	BlUser         []*BlockUser      `json:"block_user" gorm:"foreignKey:MatchingID;references:ID"`
	OnlineSoftware []*OnlineSoftware `json:"online_software" gorm:"foreignKey:MatchingID;references:ID"`
}

type BlockUser struct {
	ID         uint  `gorm:"primary_key" json:"id"`
	MatchingID uint  `json:"matching_id"`
	BlUser     int64 `json:"bl_user"`
}

type OnlineSoftware struct {
	ID         uint   `gorm:"primary_key" json:"id"`
	MatchingID uint   `json:"matching_id"`
	Name       string `json:"name"`
	Type       int8   `json:"type"` // 0 主副皆可 1 仅主 2 仅副
}

var (
	db    *gorm.DB
	getDB = fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		db, _ = gorm.Open("sqlite3", engine.DataFolder()+"matching.db")

		if err := db.AutoMigrate(&User{}, &OnlineSoftware{}, &BlockUser{}, RejectedMatchUser{}).Error; err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return false
		}
		return true
	})
)

func (b *BlockUser) TableName() string {
	return "block_user"
}

func (o *OnlineSoftware) TableName() string {
	return "online_software"
}
