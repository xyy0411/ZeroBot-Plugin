package cache

import (
	"errors"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"github.com/jinzhu/gorm"
	"time"
)

type DB struct {
	*gorm.DB
}

func NewDB(path string) *DB {
	db, err := gorm.Open("sqlite3", path)
	if err != nil {
		panic(err)
	}
	if err = db.AutoMigrate(&model.IllustCache{}, &model.SentImage{}, &model.RefreshToken{}).Error; err != nil {
		panic(err)
	}
	sqlDB := db.DB()
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	db.LogMode(false)
	return &DB{db}
}

func (db *DB) CountIllustsSmart(gid int64, keyword string, r18Req bool) (int64, error) {
	var count int64

	query := db.buildIllustQuery(gid, keyword, r18Req)
	err := query.Count(&count).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	return count, nil
}

// FindIllustsSmart 查找缓存（先 keyword，再 tags）
func (db *DB) FindIllustsSmart(gid int64, keyword string, limit int, r18Req bool) ([]model.IllustCache, error) {
	var illusts []model.IllustCache

	query := db.buildIllustQuery(gid, keyword, r18Req)
	err := query.Limit(limit).Find(&illusts).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return illusts, nil
}

// buildIllustQuery 封装基础查询逻辑（keyword + tags + 已发送过滤 + R18过滤）
func (db *DB) buildIllustQuery(gid int64, keyword string, r18Req bool) *gorm.DB {
	sub := db.Model(&model.SentImage{}).
		Where("group_id = ?", gid).
		Select("pid").
		SubQuery()

	query := db.Model(&model.IllustCache{}).
		Where("pid NOT IN (?)", sub).
		Where("(keyword = ?) OR (keyword <> ? AND tags LIKE ?)", keyword, keyword, "%"+keyword+"%")

	if !r18Req {
		query = query.Where("r18 = ?", false)
	}
	return query
}

func (db *DB) FindCached(keyword string) []int64 {
	var cachedIds []int64
	_ = db.Model(&model.IllustCache{}).
		Where("keyword = ?", keyword).
		Pluck("pid", &cachedIds).Error
	return cachedIds
}

func (db *DB) GetSentPictureIDs(gid int64) ([]int64, error) {
	var pictureIDs []int64
	err := db.Model(&model.SentImage{}).Where("group_id = ?", gid).Pluck("pid", &pictureIDs).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return pictureIDs, nil
}
