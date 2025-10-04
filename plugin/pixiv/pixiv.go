package pixiv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jinzhu/gorm"
	"io"
	"net/http"
	"net/url"
)

const maxImageSize = 20 << 20

func FetchPixivRecommend(limit int) ([]IllustCache, error) {
	firstURL := "https://app-api.pixiv.net/v1/illust/recommended?filter=for_ios"
	illustSummaries, err := fetchPixivCommon(firstURL, limit, nil, nil) // 不做R18过滤，不排缓存
	if err != nil {
		return nil, err
	}
	var result []IllustCache

	for _, r := range illustSummaries {
		Illust := IllustCache{
			PID:         r.PID,
			Keyword:     r.Tags[0],
			Title:       r.Title,
			AuthorName:  r.AuthorName,
			ImageURL:    r.ImageUrl,
			OriginalURL: r.OriginalUrl,
			Bookmarks:   r.TotalBookmarks,
			TotalView:   r.TotalView,
			CreateDate:  r.CreateDate,
			PageCount:   r.PageCount,
			R18:         r.R18,
		}
		result = append(result, Illust)
		_ = db.Create(&Illust).Error
	}
	return result, nil
}

func fetchPixivCommon(
	firstURL string,
	limit int,
	isR18Req *bool, // nil 表示不做R18过滤，true/false 表示要求
	excludeCache map[int64]struct{}, // nil表示不做缓存排除
) ([]IllustSummary, error) {
	accessToken, err := tokenResp.GetAccessToken()
	if err != nil {
		return nil, err
	}

	results := make([]IllustSummary, 0, limit)
	seen := make(map[int64]struct{})
	url := firstURL

	for len(results) < limit && url != "" {
		rawData, err := SearchPixivIllustrations(accessToken, url)
		if err != nil {
			return nil, err
		}

		for _, illust := range rawData.Illusts {
			if illust.TotalBookmarks < 1000 {
				continue
			}

			if _, ok := seen[illust.Id]; ok {
				continue
			}
			if excludeCache != nil {
				if _, ok := excludeCache[illust.Id]; ok {
					continue
				}
			}

			originalUrl := illust.MetaSinglePage.OriginalImageUrl
			if originalUrl == "" && len(illust.MetaPages) > 0 {
				originalUrl = illust.MetaPages[0].ImageURLs.Original
			}

			summary := ToIllustSummary(illust, originalUrl)

			// ✅ R18 过滤
			if isR18Req != nil {
				summary.R18 = illust.XRestrict == 1
				for _, tag := range illust.Tags {
					if isR18(tag.Name) {
						summary.R18 = true
						break
					}
				}
				if summary.R18 != *isR18Req {
					continue
				}
			}

			results = append(results, summary)
			seen[illust.Id] = struct{}{}

			if len(results) >= limit {
				break
			}
		}

		url = rawData.NextUrl
	}

	return results, nil
}

// BackgroundCacheFiller 异步补充指定关键词的 Pixiv 图片缓存
func BackgroundCacheFiller(keyword string, minCache int, r18Req bool, fetchCount int, gid int64) {
	go func() {

		var count int64

		// 第一步：先查缓存（排除掉已发送的）
		sub := db.Model(&SentImage{}).
			Where("group_id = ?", gid).
			Select("pid").
			SubQuery()

		query := db.
			Where("keyword = ?", keyword).
			Where("pid NOT IN (?)", sub)

		// 添加R-18过滤条件
		if !r18Req {
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

		newIllusts, err := FetchPixivIllusts(keyword, r18Req, fetchCount)
		if err != nil {
			fmt.Println("后台补充缓存失败:", err)
			return
		}
		if len(newIllusts) == 0 {
			fmt.Println("后台补充缓存：没有新图")
			return
		}

		var illustInfos []IllustCache
		SavePixivImgToDB(keyword, &illustInfos, newIllusts)

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}

// FetchPixivIllusts 拉取 Pixiv 插画并缓存
func FetchPixivIllusts(keyword string, isR18Req bool, limit int) ([]IllustSummary, error) {
	// 只取当前 keyword 的缓存
	var cachedIds []int64
	if err := db.Model(&IllustCache{}).
		Where("keyword = ?", keyword).
		Pluck("pid", &cachedIds).Error; err != nil {
		return nil, err
	}

	cachedMap := make(map[int64]struct{}, len(cachedIds))
	for _, id := range cachedIds {
		cachedMap[id] = struct{}{}
	}

	firstURL := BuildPixivSearchURL(keyword)
	return fetchPixivCommon(firstURL, limit, &isR18Req, cachedMap)
}

// GetIllustsByKeyword 根据关键词获取插画（优先缓存，没有则从Pixiv拉取）
func GetIllustsByKeyword(keyword string, r18Req bool, limit int, gid int64) ([]IllustCache, error) {
	var illustInfos []IllustCache

	// 第一步：先查缓存（排除掉已发送的）
	sub := db.Model(&SentImage{}).
		Where("group_id = ?", gid).
		Select("pid").
		SubQuery()

	query := db.
		Where("keyword = ?", keyword).
		Where("pid NOT IN (?)", sub)

	// 添加R-18过滤条件
	if !r18Req {
		query = query.Where("r18 = ?", false)
		fmt.Println("过滤18+")
	}

	err := query.Limit(limit).Find(&illustInfos).Error

	if !errors.Is(gorm.ErrRecordNotFound, err) && err != nil {
		return nil, err
	}

	// 如果查到了，直接返回
	if len(illustInfos) == limit {
		return illustInfos, nil
	}

	// 计算还需要几张图片
	if len(illustInfos) > 0 && len(illustInfos) < limit {
		limit -= len(illustInfos)
	}

	fmt.Printf("从数据库读到%d,还需要下载%d\n", len(illustInfos), limit)
	// 缓存没数据 -> 调用Pixiv API拉取
	pixivResults, err := FetchPixivIllusts(keyword, r18Req, limit)
	if err != nil {
		return nil, err
	}

	// 如果Pixiv也没查到直接返回空
	if len(pixivResults) == 0 && len(illustInfos) == 0 {
		return nil, fmt.Errorf("这个关键词可能没有找到符合条件的图片或出现未知错误")
	}

	if len(illustInfos) > 0 && len(pixivResults) == 0 {
		fmt.Println("http没有找到图片")
		return illustInfos, nil
	}

	// 第三步：把拉取到的数据存到缓存表
	SavePixivImgToDB(keyword, &illustInfos, pixivResults)

	if len(illustInfos) > limit {
		illustInfos = illustInfos[:limit]
	}

	fmt.Println("预计发送", len(illustInfos), "张图片")

	return illustInfos, nil
}

func (c *IllustCache) FetchPixivImage() ([]byte, error) {
	client := NewClient()

	fmt.Println("下载", c.PID)
	data, err := c.fetchImg(client, c.OriginalURL)
	if err != nil {
		/*		fmt.Println("下载的是缩略图")
				data, err = c.fetchImg(client, c.ImageURL)
				if err != nil {
					return nil, err
				}*/
		return nil, err
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
		return nil, fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}

	// 如果图片 > 20mb 就下载缩略图
	if resp.ContentLength > 0 && resp.ContentLength > maxImageSize {
		return c.fetchImg(client, c.ImageURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	/*	var builder strings.Builder
		builder.WriteString("base64://")
		base64Encoder := base64.NewEncoder(base64.StdEncoding, &builder)
		base64Encoder.Close()

		_, err = io.Copy(base64Encoder, resp.Body)
		if err != nil {
			return "", err
		}*/

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

	client := NewClient()
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

func SavePixivImgToDB(keyword string, illustInfos *[]IllustCache, pixivResults []IllustSummary) {

	existPID := make(map[int64]struct{})
	for _, r := range pixivResults {
		if _, exists := existPID[r.PID]; exists {
			continue
		}
		Illust := IllustCache{
			PID:         r.PID,
			Keyword:     keyword,
			Title:       r.Title,
			AuthorName:  r.AuthorName,
			ImageURL:    r.ImageUrl,
			OriginalURL: r.OriginalUrl,
			Bookmarks:   r.TotalBookmarks,
			TotalView:   r.TotalView,
			CreateDate:  r.CreateDate,
			PageCount:   r.PageCount,
			R18:         r.R18,
		}

		*illustInfos = append(*illustInfos, Illust)

		existPID[r.PID] = struct{}{}

		// 插入 DB，出错就忽略（可能是唯一索引冲突）
		_ = db.Create(&Illust).Error
	}
}
