package pixiv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const maxImageSize = 20 << 20

func FetchPixivByPID(pid int64) (*IllustCache, error) {
	url := fmt.Sprintf("https://app-api.pixiv.net/v1/illust/detail?illust_id=%d", pid)
	accessToken, err := tokenResp.GetAccessToken()
	if err != nil {
		return nil, err
	}
	rawData, err := SearchPixivIllustrations(accessToken, url)
	if err != nil {
		return nil, err
	}
	if rawData == nil || rawData.Illust == nil {
		return nil, fmt.Errorf("pixiv 返回数据为空或结构不匹配")
	}
	illust := *rawData.Illust

	Illust, err := convertToIllustCache(illust)

	return &Illust, err
}

func FetchPixivByUser(uid, gid int64, limit int) ([]IllustCache, error) {
	url := fmt.Sprintf("https://app-api.pixiv.net/v1/user/illusts?user_id=%d&type=illust", uid)

	excludeCache := make(map[int64]struct{})

	var pids []int64
	err := db.Model(&SentImage{}).Where("group_id = ?", gid).Pluck("pid", &pids).Error
	if err != nil {
		return nil, err
	}
	for _, pid := range pids {
		excludeCache[pid] = struct{}{}
	}
	isR18Req := true
	return fetchPixivCommon(url, limit, &isR18Req, excludeCache)
}

func FetchPixivRecommend(limit int) ([]IllustCache, error) {
	firstURL := "https://app-api.pixiv.net/v1/illust/recommended?filter=for_ios"
	illustSummaries, err := fetchPixivCommon(firstURL, limit, nil, nil) // 不做R18过滤，不排缓存
	if err != nil {
		return nil, err
	}
	return illustSummaries, nil
}

func fetchPixivCommon(
	firstURL string,
	limit int,
	isR18Req *bool, // nil 表示不做R18过滤，true/false 表示要求
	excludeCache map[int64]struct{}, // nil表示不做缓存排除
	keywords ...string,
) ([]IllustCache, error) {

	accessToken, err := tokenResp.GetAccessToken()
	if err != nil {
		return nil, err
	}

	results := make([]IllustCache, 0, limit)
	seen := make(map[int64]struct{})
	url := firstURL

	for len(results) < limit && url != "" {
		rawData, err := SearchPixivIllustrations(accessToken, url)
		if err != nil {
			return nil, err
		}

		for _, raw := range rawData.Illusts {
			if raw.TotalBookmarks < 1000 {
				continue
			}

			if _, ok := seen[raw.Id]; ok {
				continue
			}
			if excludeCache != nil {
				if _, ok := excludeCache[raw.Id]; ok {
					continue
				}
			}

			Illust, err := convertToIllustCache(raw)
			if err != nil {
				return nil, err
			}

			if len(keywords) > 0 {
				Illust.Keyword = keywords[0]
			}

			// ✅ R18 过滤
			if isR18Req != nil && Illust.R18 != *isR18Req {
				continue
			}

			_ = db.Create(&Illust).Error

			results = append(results, Illust)
			seen[raw.Id] = struct{}{}

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

		count, err := CountIllustsSmart(gid, keyword, r18Req)
		if err != nil {
			fmt.Println("查询数据库发生错误:", err)
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

		fmt.Printf("后台成功补充 %d 张图片到关键词 %s 缓存\n", len(newIllusts), keyword)
	}()
}

// FetchPixivIllusts 拉取 Pixiv 插画并缓存
func FetchPixivIllusts(keyword string, isR18Req bool, limit int) ([]IllustCache, error) {
	// 只取当前 keyword 的缓存
	var cachedIds []int64
	if err := db.Model(&IllustCache{}).
		Where("keyword = ?", keyword).
		Pluck("pid", &cachedIds).Error; err != nil {
		return nil, err
	}

	cachedMap := make(map[int64]struct{}, len(cachedIds))
	if len(cachedIds) > 0 {
		for _, id := range cachedIds {
			cachedMap[id] = struct{}{}
		}
	}

	firstURL := BuildPixivSearchURL(keyword)
	return fetchPixivCommon(firstURL, limit, &isR18Req, cachedMap, keyword)
}

// GetIllustsByKeyword 根据关键词获取插画（优先缓存，没有则从Pixiv拉取）
func GetIllustsByKeyword(keyword string, r18Req bool, limit int, gid int64) ([]IllustCache, error) {

	// 设置一个保底的关键词
	if keyword == "" && r18Req {
		keyword = "R-18"
	}

	illustInfos, err := FindIllustsSmart(gid, keyword, limit, r18Req)
	if err != nil {
		return nil, err
	}

	// 如果查到了，直接返回
	if len(illustInfos) == limit {
		return illustInfos, nil
	}

	// 计算还需要几张图片
	needed := 0
	if len(illustInfos) < limit {
		needed = limit - len(illustInfos)
	}

	fmt.Printf("从数据库读到%d,还需要下载%d\n", len(illustInfos), needed)
	// 缓存没数据 -> 调用Pixiv API拉取
	pixivResults, err := FetchPixivIllusts(keyword, r18Req, needed)
	if err != nil && len(pixivResults) == 0 {
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

	illustInfos = append(illustInfos, pixivResults...)

	if len(illustInfos) >= limit {
		illustInfos = illustInfos[:limit]
	}

	fmt.Println("预计发送", len(illustInfos), "张图片")

	return illustInfos, nil
}

func (c *IllustCache) FetchPixivImage(preferOriginal ...bool) ([]byte, error) {
	fmt.Println("下载", c.PID)

	if c == nil {
		fmt.Println("FetchPixivImage called on nil IllustCache")
		return nil, nil
	}

	preferOriginalFlag := false

	if len(preferOriginal) > 0 {
		preferOriginalFlag = preferOriginal[0]
	}

	data, err := c.fetchImg(c.OriginalURL, preferOriginalFlag)
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

func (c *IllustCache) fetchImg(url string, preferOriginal bool) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Referer", "https://www.pixiv.net/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	client := NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}

	if !preferOriginal {
		// 如果图片 > 20mb 就下载缩略图
		if resp.ContentLength > 0 && resp.ContentLength > maxImageSize {
			return c.fetchImg(c.ImageURL, preferOriginal)
		}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 用base64发成功概率很小
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
