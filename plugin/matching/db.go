package matching

import (
	"errors"

	fcext "github.com/FloatTech/floatbox/ctxext"
	"github.com/jinzhu/gorm"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var (
	db    *gorm.DB
	getDB = fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		db, _ = gorm.Open("sqlite3", engine.DataFolder()+"matching.db")

		if err := db.AutoMigrate(RejectedMatchUser{}).Error; err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return false
		}
		return true
	})
)

func isRejectedMatchUser(uid int64) (bool, error) {
	err := db.Where("user_id = ?", uid).First(&RejectedMatchUser{}).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func addRejectedMatchUser(uid int64) error {
	return db.Save(&RejectedMatchUser{UserID: uid}).Error
}

func removeRejectedMatchUser(uid int64) error {
	return db.Where("user_id = ?", uid).Delete(&RejectedMatchUser{}).Error
}
