package proxy

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (m *Manager) DownloadingNode(url string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "v2rayN/5.38")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.Join(errors.New("status code: "+resp.Status), errors.New("url: "+url))
	}
	rawData, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	nodes, err := parseSubscription(string(rawData))
	if err != nil {
		return err
	}

	tx := m.db.Begin()
	if err := tx.Error; err != nil {
		return err
	}

	if err := tx.Delete(&model.Node{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	if len(nodes) > 0 {
		for i := range nodes {
			node := nodes[i]
			if err := tx.Create(&node).Error; err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	return tx.Commit().Error
}

// 解析 VMess 节点
func parseVMess(line string) (model.Node, error) {
	b64 := strings.TrimPrefix(line, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return model.Node{}, err
	}

	var vm map[string]string
	if err := json.Unmarshal(data, &vm); err != nil {
		return model.Node{}, err
	}

	return model.Node{
		Protocol: "vmess",
		Name:     vm["ps"],
		Address:  vm["add"],
		Port:     vm["port"],
		ID:       vm["id"],
		Network:  vm["net"],
		Host:     vm["host"],
		Path:     vm["path"],
		TLS:      vm["tls"],
		Sni:      vm["sni"],
	}, nil
}

// 解析 VLESS 节点
func parseVLESS(line string) (model.Node, error) {
	raw := strings.TrimPrefix(line, "vless://")

	u, err := url.Parse(raw)
	if err != nil {
		return model.Node{}, err
	}

	id := u.User.Username()
	address := u.Hostname()
	port := u.Port()
	name := u.Fragment

	query := u.Query()
	return model.Node{
		Protocol: "vless",
		Name:     name,
		Address:  address,
		Port:     port,
		ID:       id,
		Network:  query.Get("type"),
		Host:     query.Get("host"),
		Path:     query.Get("path"),
		TLS:      query.Get("security"),
		Sni:      query.Get("sni"),
	}, nil
}

func testNode(node model.Node, timeout time.Duration) (float64, error) {
	addr := net.JoinHostPort(node.Address, node.Port)
	start := time.Now()

	var conn net.Conn
	var err error
	if node.TLS == "tls" || node.TLS == "TLS" {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, &tls.Config{
			ServerName:         node.Host,
			InsecureSkipVerify: true,
		})
	} else {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	}
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return float64(time.Since(start).Milliseconds()), nil
}
