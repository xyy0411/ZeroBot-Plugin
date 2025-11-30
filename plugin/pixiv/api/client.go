package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maxImageSize = 20 << 20

// Client 封装 HTTP 客户端与 Pixiv 请求逻辑
type Client struct {
	*http.Client
}

func NewClient(proxyUrl string) *Client {
	proxyURL, err := url.Parse(proxyUrl)
	if err != nil {
		log.Warning("连接代理错误:", err)
	}

	return &Client{
		&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS13},
				Proxy:           http.ProxyURL(proxyURL),
			},
			Timeout: time.Minute,
		},
	}
}

func (c *Client) SearchPixivIllustrations(accessToken, url string) (*model.RootEntity, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")
	req.Header.Set("App-OS", "android")
	req.Header.Set("App-OS-Version", "11")
	req.Header.Set("App-Version", "5.0.234")

	req.Header.Set("Accept-Language", "en_US")
	req.Header.Set("Referer", "https://app-api.pixiv.net/")
	req.Header.Set("Connection", "keep-alive")

	req.Host = "app-api.pixiv.net"

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("搜索失败: %s\nbody: %s", resp.Status, string(body))
	}

	var result model.RootEntity
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) FetchPixivImage(illust model.IllustCache, url string, preferOriginal ...bool) ([]byte, error) {
	fmt.Println("下载", illust.PID)

	if c == nil {
		fmt.Println("FetchPixivImage called on nil IllustCache")
		return nil, nil
	}

	preferOriginalFlag := false

	if len(preferOriginal) > 0 {
		preferOriginalFlag = preferOriginal[0]
	}

	req, _ := http.NewRequest("GET", url, nil)

	req.Header.Set("Referer", "https://www.pixiv.net/")
	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}

	if !preferOriginalFlag {
		// 如果图片 > 20mb 就下载缩略图
		if resp.ContentLength > 0 && resp.ContentLength > maxImageSize {
			return c.FetchPixivImage(illust, illust.ImageURL, preferOriginalFlag)
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
