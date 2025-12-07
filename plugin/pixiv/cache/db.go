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
	if err = db.AutoMigrate(&model.IllustCache{}, &model.SentImage{}, &model.RefreshToken{}, &model.Node{}).Error; err != nil {
		panic(err)
	}
	sqlDB := db.DB()
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	db.LogMode(false)
	return &DB{db}
}

func (db *DB) FindByKeyword(gid int64, keyword string, limit int, r18Req bool) ([]model.IllustCache, error) {
	var results []model.IllustCache
	query := db.Model(&model.IllustCache{}).
		Where("keyword = ?", keyword).
		Where("pid NOT IN (?)", db.Model(&model.SentImage{}).Where("group_id = ?", gid).Select("pid").SubQuery()).
		Order("bookmarks DESC").
		Limit(limit)

	query = query.Where("r18 = ?", r18Req)

	err := query.Find(&results).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return results, nil
}

func (db *DB) FindByTag(gid int64, tag string, needed int, r18Req bool) ([]model.IllustCache, error) {
	if needed <= 0 {
		return nil, nil
	}
	var results []model.IllustCache

	query := db.Model(&model.IllustCache{}).
		Where("tags LIKE ?", "%"+tag+"%").
		Where("pid NOT IN (?)", db.Model(&model.SentImage{}).Where("group_id = ?", gid).Select("pid").SubQuery()).
		Order("bookmarks DESC").
		Limit(needed)

	if !r18Req {
		query = query.Where("r18 = ?", false)
	}

	err := query.Find(&results).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return results, nil
}
func (db *DB) CountIllustsSmart(gid int64, keyword string, r18Req bool) (int64, error) {
	var count int64

	query := db.Model(&model.IllustCache{}).
		Where("keyword = ?", keyword).
		Where("pid NOT IN (?)", db.Model(&model.SentImage{}).Where("group_id = ?", gid).Select("pid").SubQuery())

	if !r18Req {
		query = query.Where("r18 = ?", false)
	}
	err := query.Count(&count).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	return count, nil
}

func (db *DB) FindIllustsSmart(gid int64, keyword string, limit int, r18Req bool) ([]model.IllustCache, error) {
	seen := make(map[int64]struct{})
	var results []model.IllustCache

	// 1. keyword 严格查询
	kwRes, err := db.FindByKeyword(gid, keyword, limit, r18Req)
	if err != nil {
		return nil, err
	}
	for _, ill := range kwRes {
		results = append(results, ill)
		seen[ill.PID] = struct{}{}
	}

	// 已经满足 limit
	if len(results) >= limit {
		return results[:limit], nil
	}

	// 2. tag 查询补齐
	need := limit - len(results)
	tagRes, err := db.FindByTag(gid, keyword, need, r18Req)
	if err != nil {
		return nil, err
	}

	for _, ill := range tagRes {
		if _, ok := seen[ill.PID]; ok {
			continue
		}
		results = append(results, ill)
		seen[ill.PID] = struct{}{}
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (db *DB) GetIllustIDsByKeyword(keyword string) ([]int64, error) {
	var illustIDs []int64
	err := db.Model(&model.IllustCache{}).Where("keyword = ?", keyword).Pluck("pid", &illustIDs).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return illustIDs, nil
}

func (db *DB) GetSentPictureIDs(gid int64) ([]int64, error) {
	var pictureIDs []int64
	err := db.Model(&model.SentImage{}).Where("group_id = ?", gid).Pluck("pid", &pictureIDs).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return pictureIDs, nil
}

// DeleteIllustByPID 删除指定 PID 的插画缓存记录
func (db *DB) DeleteIllustByPID(pid int64) error {
	return db.Where("pid = ?", pid).Delete(&model.IllustCache{}).Error
}
