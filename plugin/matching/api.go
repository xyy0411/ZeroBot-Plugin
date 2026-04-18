package matching

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func apiURL(path string) string {
	return matchingAPIBase + path
}

func isUserInMatchingQueue(uid int64) (bool, string, error) {
	status, body, err := doRequest(http.MethodGet, "/status/"+strconv.FormatInt(uid, 10), nil, "")
	if err != nil {
		return false, "", err
	}
	return status == http.StatusOK, strings.TrimSpace(string(body)), nil
}

func doMatchInfo(uid int64) (m Matching, err error) {
	resp, err := http.Get(apiURL("/profile/" + strconv.FormatInt(uid, 10)))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("status code: %d", resp.StatusCode)
	}
	all, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	m1 := make(map[string]any)
	err = json.Unmarshal(all, &m1)
	if err != nil {
		return
	}
	m2 := m1["data"].(map[string]any)["matching"]
	marshal, err := json.Marshal(m2)
	if err != nil {
		return
	}
	err = json.Unmarshal(marshal, &m)
	return
}

func doRequest(method, path string, body io.Reader, contentType string) (int, []byte, error) {
	req, err := http.NewRequest(method, apiURL(path), body)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, respBody, nil
}

func doJSON(method, path string, payload any) (int, []byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	return doRequest(method, path, bytes.NewReader(data), "application/json")
}

func ensureProfile(userID int64, userName string, limitTime int64) error {
	_, err := doMatchInfo(userID)
	if err == nil {
		return nil
	}
	status, body, err := doJSON(http.MethodPost, "/profile", map[string]any{
		"user_id":   userID,
		"user_name": userName,
		"expire_at": limitTime,
	})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusConflict || status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		text := strings.ToLower(string(body))
		if strings.Contains(text, "exists") || strings.Contains(text, "已存在") {
			return nil
		}
	}
	return fmt.Errorf("create profile failed: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func updateExpire(userID int64, seconds int64) (string, error) {
	status, body, err := doJSON(http.MethodPatch, "/profile/"+strconv.FormatInt(userID, 10)+"/expire", map[string]any{
		"expire_at": seconds,
	})
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return responseMessage(body), nil
	}
	return "", fmt.Errorf("更新匹配时间失败: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func addSoftware(userID int64, software string, softwareType int8) (string, error) {
	status, body, err := doJSON(http.MethodPost, "/profile/"+strconv.FormatInt(userID, 10)+"/software", map[string]any{
		"name":          software,
		"software_name": software,
		"type":          softwareType,
	})
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return responseMessage(body), nil
	}
	return "", fmt.Errorf("添加匹配软件失败: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func updateName(userID int64, userName string) (string, error) {
	var input struct {
		UserName string `json:"user_name"`
	}
	input.UserName = userName
	data, err := json.Marshal(&input)
	if err != nil {
		return "", err
	}
	status, body, err := doRequest(http.MethodPatch, "/profile/"+strconv.FormatInt(userID, 10), bytes.NewReader(data), "application/json")
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return responseMessage(body), nil
	}
	return "", err
}

func deleteSoftware(userID int64, softwareName string) (string, error) {
	path := fmt.Sprintf("/profile/%d/software", userID)

	var input struct {
		SoftwareName string `json:"software_name"`
	}
	input.SoftwareName = softwareName

	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	status, body, err := doRequest(http.MethodDelete, path, bytes.NewReader(data), "application/json")
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return responseMessage(body), nil
	}
	return responseMessage(body), nil
}

func addBlockUser(userID, targetUserID int64) (string, error) {
	status, body, err := doJSON(http.MethodPost, "/profile/"+strconv.FormatInt(userID, 10)+"/block-user", map[string]any{
		"target_user_id": targetUserID,
	})
	if err != nil {
		return "", err
	}
	if status >= 200 && status < 300 {
		return responseMessage(body), nil
	}
	return "", fmt.Errorf("添加黑名单错误: status=%d, body=%s", status, strings.TrimSpace(string(body)))
}

func deleteBlockUser(userID, targetID int64) (string, error) {
	path := fmt.Sprintf("/profile/%d/block-user/%d", userID, targetID)
	_, body, err := doRequest(http.MethodDelete, path, nil, "")
	if err != nil {
		return "", err
	}
	return responseMessage(body), nil
}

func cancelMatching(userID int64) (string, error) {
	_, body, err := doRequest(http.MethodDelete, "/"+strconv.FormatInt(userID, 10), nil, "")
	if err != nil {
		return "", err
	}
	return responseMessage(body), nil
}

func responseMessage(body []byte) string {
	m := make(map[string]string)
	if err := json.Unmarshal(body, &m); err == nil {
		if msg := strings.TrimSpace(m["message"]); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(string(body))
}
