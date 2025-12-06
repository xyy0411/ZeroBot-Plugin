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

func NewClient(proxyUrl string) *Client {
	proxyURL, err := url.Parse(proxyUrl)
	if err != nil {
		log.Warning("连接代理错误:", err)
		proxyURL = nil
	}

	var noProxyDomains = []string{
		// 源自https://blog.yuki.sh/posts/599ec3ed8eda/
		"i.yuki.sh",
		// 源自https://i.muxmus.com/
		"i.muxmus.com",
	}

	return &Client{
		&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS13},
				Proxy: func(req *http.Request) (*url.URL, error) {
					host := req.URL.Hostname()

					for _, d := range noProxyDomains {
						if host == d {
							return nil, nil
						}
					}

					// 其它全部走代理
					return proxyURL, nil
				},
			},
			Timeout: time.Minute,
		},
	}
}

const (
	yuki   = "i.yuki.sh"
	muxmus = "i.muxmus.com"
)

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

// FetchPixivImage 优先直连 P 站下载，失败后依次使用反代重试
func (c *Client) FetchPixivImage(illust model.IllustCache, url string) ([]byte, bool, error) {
	fmt.Println("下载", illust.PID)

	if c == nil {
		fmt.Println("FetchPixivImage called on nil IllustCache")
		return nil, false, nil
	}

	// 1. 直连 Pixiv
	data, status, err := c.fetchOnce(url, "https://www.pixiv.net/")
	if err == nil && status == http.StatusOK {
		return data, false, nil
	}
	if status == http.StatusNotFound {
		return nil, false, &HTTPStatusError{StatusCode: status, URL: url}
	}

	fmt.Println("下载失败:", illust.PID, "准备使用反向代理")
	// 2. 反代兜底
	fallbackHosts := []string{yuki, muxmus}
	var lastStatus int
	var lastErr error

	for _, host := range fallbackHosts {
		replacedURL, err := replaceDomain(url, host)
		if err != nil {
			lastErr = err
			continue
		}

		data, status, err = c.fetchOnce(replacedURL, "https://"+host)
		if err != nil {
			lastErr = err
			continue
		}

		if status == http.StatusOK {
			return data, true, nil
		}

		if status == http.StatusNotFound {
			return nil, true, &HTTPStatusError{StatusCode: status, URL: replacedURL}
		}

		lastStatus = status
	}

	if lastErr != nil {
		return nil, true, lastErr
	}

	if lastStatus != 0 {
		return nil, true, &HTTPStatusError{StatusCode: lastStatus, URL: url}
	}

	return nil, true, fmt.Errorf("下载图片失败")
}
