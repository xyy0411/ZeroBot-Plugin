package proxy

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	nodesFile        = "/root/v2ray/nodes.json"
	currentIndexFile = "/root/v2ray/current_index.txt"
	configFile       = "/usr/local/etc/v2ray/config.json"
)

// Node 通用节点结构
type Node struct {
	Protocol string `json:"protocol"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Port     string `json:"port"`
	ID       string `json:"id"`
	Network  string `json:"network"`
	Host     string `json:"host"`
	Path     string `json:"path"`
	TLS      string `json:"tls"`
	Sni      string `json:"sni"`

	DelayMs float64 `json:"-"`
}

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
	nodesBytes, err := json.Marshal(nodes)
	if err != nil {
		return err
	}
	openFile, err := os.OpenFile(nodesFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer openFile.Close()
	_, err = openFile.Write(nodesBytes)
	return err
}

// 解析 VMess 节点
func parseVMess(line string) (Node, error) {
	b64 := strings.TrimPrefix(line, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return Node{}, err
	}

	var vm map[string]string
	if err := json.Unmarshal(data, &vm); err != nil {
		return Node{}, err
	}

	return Node{
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
func parseVLESS(line string) (Node, error) {
	raw := strings.TrimPrefix(line, "vless://")

	u, err := url.Parse(raw)
	if err != nil {
		return Node{}, err
	}

	id := u.User.Username()
	address := u.Hostname()
	port := u.Port()
	name := u.Fragment

	query := u.Query()
	return Node{
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

func testNode(node Node, timeout time.Duration) (float64, error) {
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
