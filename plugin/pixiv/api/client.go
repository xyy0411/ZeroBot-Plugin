package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"io"
	"net/http"
	"time"
)

// HTTPStatusError 捕获图片下载时的 HTTP 状态码
type HTTPStatusError struct {
	StatusCode int
	URL        string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("下载图片失败: HTTP %d", e.StatusCode)
}

// Client 封装 HTTP 客户端与 Pixiv 请求逻辑
type Client struct {
	*http.Client
}

func NewClient() *Client {
	return &Client{
		&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS13},
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

func (c *Client) fetchOnce(targetURL, referer string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", "PixivAndroidApp/5.0.234 (Android 11; Pixel 5)")

	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return data, resp.StatusCode, nil
}

// FetchPixivImage 直接从 Pixiv 下载图片
func (c *Client) FetchPixivImage(illust model.IllustCache, url string) ([]byte, error) {
	fmt.Println("下载", illust.PID)

	if c == nil {
		fmt.Println("FetchPixivImage called on nil IllustCache")
		return nil, nil
	}

	data, status, err := c.fetchOnce(url, "https://www.pixiv.net/")
	if err == nil && status == http.StatusOK {
		return data, nil
	}
	if status == http.StatusNotFound {
		return nil, &HTTPStatusError{StatusCode: status, URL: url}
	}

	if err != nil {
		return nil, err
	}

	if status != 0 {
		return nil, &HTTPStatusError{StatusCode: status, URL: url}
	}

	return nil, fmt.Errorf("下载图片失败")
}
