package chatgpt

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

const (
	// baseURL  = "https://api.openai.com/v1/"
	//proxyURL           = "https://uu.ci/v1/chat/completions"
	proxyURL           = "https://api.alioth.center/akasha-whisper/v1/chat/completions"
	modelGPT3Dot5Turbo = "gpt3.5-turbo"
	yunKey             = "7d06a110e9e20a684e02934549db1d3d"
	yunURL             = "https://api.a20safe.com/api.php?api=35&key=%s&apikey=%s"
)

type yun struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		Return    string `json:"return"`
		Total     string `json:"total"`
		Available string `json:"available"`
		Used      string `json:"used"`
	} `json:"data"`
}

// tts语言回复
/*type ApifoxModel struct {
	// 要生成音频的文本。最大长度为4096个字符。
	Input string `json:"input"`
	Model string `json:"model"`
	// 生成音频时使用的语音。支持的语音有:alloy、echo、fable、onyx、nova 和 shimmer。
	Voice string `json:"voice"`
}
*/
// chatGPTResponseBody 响应体
type chatGPTResponseBody struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int          `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

// chatGPTRequestBody 请求体
type chatGPTRequestBody struct {
	Model    string        `json:"model,omitempty"` // gpt3.5-turbo || gpt-4
	Messages []chatMessage `json:"messages,omitempty"`
}

// chatMessage 消息
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatChoice struct {
	Index        int `json:"index"`
	Message      chatMessage
	FinishReason string `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

var client = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	},
	Timeout: time.Minute * 5,
}

// completions gtp3.5文本模型回复
func completions(messages []chatMessage, apiKey string, model string, url string) (*chatGPTResponseBody, error) {
	com := chatGPTRequestBody{
		Messages: messages,
		Model:    model,
	}
	body, err := json.Marshal(com)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", apiKey)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	/*req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")*/
	//client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusBadRequest {
		return nil, errors.New("response error: " + strconv.Itoa(res.StatusCode))
	}
	v := new(chatGPTResponseBody)
	if err = json.NewDecoder(res.Body).Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
