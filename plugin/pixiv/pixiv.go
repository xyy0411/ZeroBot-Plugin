package pixiv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/FloatTech/floatbox/web"
	"github.com/jinzhu/gorm"
	"io"
	"net/http"
	"net/url"
)

// BackgroundCacheFiller 异步补充指定关键词的 Pixiv 图片缓存
func BackgroundCacheFiller(keyword string, minCache int, fetchCount int, gid int64) {
	go func() {

		var count int64

		filterR18 := requiresNonR18(keyword)

		// 第一步：先查缓存（排除掉已发送的）
		sub := db.Model(&SentImage{}).
			Where("group_id = ?", gid).
			Select("pid").
			SubQuery()

		query := db.
			Where("keyword = ?", keyword).
			Where("pid NOT IN (?)", sub)

		// 添加R-18过滤条件
		if filterR18 {
			query = query.Where("r18 = ?", false)
		}

		err := query.Model(&IllustCache{}).Count(&count).Error

		if !errors.Is(err, gorm.ErrRecordNotFound) && err != nil {
			fmt.Println("统计缓存数量失败:", err)
			return
		}

		if count >= int64(minCache) {
			fmt.Println("缓存足够，无需补充:", keyword)
			return
		}

		fmt.Printf("后台补充关键词 %s, 数量 %d\n", keyword, fetchCount)

		newIllusts, err := FetchPixivIllusts(keyword, requiresNonR18(keyword), fetchCount)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)
			return
		}
		if len(newIllusts) == 0 {
			fmt.Println("后台补充缓存：没有新图")
			return
		}

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}

// FetchPixivIllusts 拉取 Pixiv 插画并缓存
func FetchPixivIllusts(keyword string, isR18 bool, limit int) (results []IllustSummary, err error) {
	seen := make(map[int64]struct{})
	fetched := 0

	accessToken, err := tokenResp.GetAccessToken()
	if err != nil {
		return nil, err
	}

	// 只取当前 keyword 的缓存
	var cachedIds []int64
	if err = db.Model(&IllustCache{}).
		Where("keyword = ?", keyword).
		Pluck("pid", &cachedIds).Error; err != nil {
		return nil, err
	}

	cachedMap := make(map[int64]struct{}, len(cachedIds))
	for _, id := range cachedIds {
		cachedMap[id] = struct{}{}
	}

	nextURL := BuildPixivSearchURL(keyword)
	for fetched <= limit && nextURL != "" {
		rawData, err := SearchPixivIllustrations(accessToken, nextURL)
		if err != nil {
			return nil, err
		}

		for _, illust := range rawData.Illusts {
			// 可能会在下一页出现相同的插画这里也要跳
			if _, ok := seen[illust.Id]; ok {
				continue
			}
			// 缓存有就跳
			if _, ok := cachedMap[illust.Id]; ok {
				continue
			}

			summary := ToIllustSummary(illust)
			results = append(results, summary)

			cache := IllustCache{
				PID:        illust.Id,
				Keyword:    keyword,
				Title:      illust.Title,
				AuthorName: illust.User.Name,
				ImageURL:   illust.ImageUrls.Large,
				// Original:   original,
				Bookmarks:  illust.TotalBookmarks,
				TotalView:  illust.TotalView,
				CreateDate: illust.CreateDate,
				PageCount:  illust.PageCount,
			}

			// 判断作品是不是有多张图如果是多张就取第一张为原图
			if illust.MetaSinglePage.OriginalImageUrl == "" {
				cache.Original = illust.MetaPages[0].ImageURLs.Original
			} else {
				cache.Original = illust.MetaSinglePage.OriginalImageUrl
			}

			// 检查是不是r18
			for _, tag := range illust.Tags {
				if !requiresNonR18(tag.Name) {
					cache.R18 = true
				}
			}

			if !(cache.R18 && isR18) {
				continue
			}

			seen[illust.Id] = struct{}{}
			fetched++

			if fetched >= limit {
				break
			}
		}
		nextURL = rawData.NextUrl
	}

	return results, nil
}

// GetIllustsByKeyword 根据关键词获取插画（优先缓存，没有则从Pixiv拉取）
func GetIllustsByKeyword(keyword string, limit int, gid int64) ([]IllustCache, error) {
	var illustInfos []IllustCache

	filterR18 := requiresNonR18(keyword)

	// 第一步：先查缓存（排除掉已发送的）
	sub := db.Model(&SentImage{}).
		Where("group_id = ?", gid).
		Select("pid").
		SubQuery()

	query := db.
		Where("keyword = ?", removeR18Keywords(keyword)).
		Where("pid NOT IN (?)", sub)

	// 添加R-18过滤条件
	if filterR18 {
		query = query.Where("r18 = ?", false)
	}

	err := query.Limit(limit).Find(&illustInfos).Error

	if !errors.Is(gorm.ErrRecordNotFound, err) && err != nil {
		return nil, err
	}

	// 如果查到了，直接返回
	if len(illustInfos) == limit {
		return illustInfos, nil
	}

	// 判断还需要几张图片
	if len(illustInfos) > 0 && len(illustInfos) < limit {
		limit -= len(illustInfos)
	}

	// 缓存没数据 -> 调用Pixiv API拉取
	pixivResults, err := FetchPixivIllusts(removeR18Keywords(keyword), requiresNonR18(keyword), limit)
	if err != nil {
		return nil, err
	}

	// 如果Pixiv也没数据，直接返回空
	if len(pixivResults) == 0 {
		return nil, fmt.Errorf("这个关键词可能没有找到符合条件的图片或出现未知错误")
	}

	r18Keywords := removeR18Keywords(keyword)

	// 第三步：把拉取到的数据存到缓存表
	for _, r := range pixivResults {
		Illust := IllustCache{
			PID:        r.PID,
			Keyword:    r18Keywords,
			Title:      r.Title,
			AuthorName: r.AuthorName,
			ImageURL:   r.ImageUrl,
			Original:   r.OriginalUrl,
			Bookmarks:  r.TotalBookmarks,
			TotalView:  r.TotalView,
			CreateDate: r.CreateDate,
			PageCount:  r.PageCount,
		}

		illustInfos = append(illustInfos, Illust)

		if err := db.Create(&Illust).Error; err != nil {
			return nil, err
		}
	}

	sub2 := db.Model(&SentImage{}).Select("pid").SubQuery()
	err = db.
		Where("keyword = ?", keyword).
		Where("pid NOT IN (?)", sub2).
		Limit(limit).
		Find(&illustInfos).Error
	if err != nil {
		return nil, err
	}

	return illustInfos, nil
}

func (c *IllustCache) FetchPixivImage() ([]byte, error) {
	client := NewClient()

	data, err := c.fetchImg(client, c.Original)
	if err != nil {
		fmt.Println("下载的是缩略图")
		data, err = c.fetchImg(client, c.ImageURL)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (c *IllustCache) fetchImg(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Referer", "https://www.pixiv.net/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取数据失败: %w", err)
	}
	return data, nil
}

// RefreshPixivAccessToken 用 refresh_token 刷新 access_token
func RefreshPixivAccessToken(refreshToken string) (*TokenStore, error) {
	endpoint := "https://oauth.secure.pixiv.net/auth/token"

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", "MOBrBDS8blbauoSck0ZfDbtuzpyT")
	data.Set("client_secret", "lsACyCD94FhDUtGTXi3QzcFE2uU1hqtDaKeqrdwj")
	data.Set("refresh_token", refreshToken)

	req, _ := http.NewRequest("POST", endpoint, bytes.NewBufferString(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")

	client := web.NewTLS12Client()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("刷新失败: %s\nbody: %s", resp.Status, string(body))
	}

	var tokenRes TokenStore
	if err := json.Unmarshal(body, &tokenRes); err != nil {
		return nil, err
	}
	return &tokenRes, nil
}

func SearchPixivIllustrations(accessToken, url string) (*RootEntity, error) {

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")

	client := NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("搜索失败: %s\nbody: %s", resp.Status, string(body))
	}

	var result RootEntity
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
