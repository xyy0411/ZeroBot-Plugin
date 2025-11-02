package pixiv

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	nodesFile        = "/usr/local/etc/v2ray/nodes.json"
	currentIndexFile = "/usr/local/etc/v2ray/current_index.txt"
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

// 并发自动测试并切换
func autoSwitchConcurrent() {
	// load nodes.json
	bs, err := os.ReadFile(nodesFile)
	if err != nil {
		fmt.Println("读取 nodes.json 失败:", err)
		return
	}
	var nodes []Node
	if err := json.Unmarshal(bs, &nodes); err != nil {
		fmt.Println("解析 nodes.json 失败:", err)
		return
	}
	if len(nodes) == 0 {
		fmt.Println("nodes.json 为空")
		return
	}

	total := len(nodes)
	okCount := 0
	failCount := 0

	fmt.Printf("开始并发检测 %d 个节点...\n", len(nodes))

	timeout := 4 * time.Second
	results := make(chan Node, len(nodes))
	var wg sync.WaitGroup

	for _, n := range nodes {
		wg.Add(1)
		go func(nd Node) {
			defer wg.Done()
			delay, err := testNode(nd, timeout)
			if err != nil {
				fmt.Printf("❌ %s 不可用: %v\n", nd.Name, err)
				failCount++
				return
			}
			nd.DelayMs = delay
			fmt.Printf("✅ %s 可用，延迟 %.1fms\n", nd.Name, nd.DelayMs)
			okCount++
			results <- nd
		}(n)
	}

	// close when done
	go func() {
		wg.Wait()
		close(results)
	}()

	// pick best (smallest delay)
	var best Node
	best.DelayMs = 1e9
	for r := range results {
		if r.DelayMs < best.DelayMs {
			best = r
		}
	}

	if best.Name == "" {
		fmt.Println("🚨 未发现可用节点")
		return
	}
	fmt.Printf("\n检测完成：共 %d 个节点，可用 %d 个，不可用 %d 个。\n", total, okCount, failCount)
	fmt.Printf("\n⚡ 最佳节点: %s, 延迟 %.1fms\n", best.Name, best.DelayMs)

	// 写配置并重启 v2ray（确保重启）
	if err := writeConfigAndRestart(best); err != nil {
		fmt.Println("⚠️ 切换到最佳节点失败:", err)
		return
	}

	fmt.Println("✅ 自动切换完成")
}

func writeConfigAndRestart(node Node) error {
	portNum := node.Port
	if _, err := strconv.Atoi(portNum); err != nil {
		portNum = "80"
	}

	security := "none"
	if node.TLS == "tls" || node.TLS == "TLS" {
		security = "tls"
	}

	config := fmt.Sprintf(`{
  "inbounds": [
    {"port":1080,"protocol":"socks","settings":{"auth":"noauth"}},
    {"port":10809,"protocol":"http","settings":{"auth":"noauth"}}
  ],
  "outbounds": [
    {
      "protocol": "vmess",
      "settings": {
        "vnext": [
          {
            "address": "%s",
            "port": %s,
            "users": [
              { "id": "%s", "alterId": 0, "security": "auto" }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "%s",
        "security": "%s",
        "wsSettings": {
          "path": "%s",
          "headers": { "Host": "%s" }
        }
      }
    }
  ]
}`, node.Address, portNum, node.ID, node.Network, security, node.Path, node.Host)

	if err := os.WriteFile(configFile, []byte(config), 0644); err != nil {
		return fmt.Errorf("write config failed: %w", err)
	}

	if err := os.WriteFile(currentIndexFile, []byte(node.Name), 0644); err != nil {
		fmt.Println("warning: write current node name failed:", err)
	}

	cmd := exec.Command("systemctl", "restart", "v2ray")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart v2ray: %v, output: %s", err, string(out))
	}

	fmt.Printf("🔄 已写入 config 并重启 v2ray，节点: %s\n", node.Name)
	return nil
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

// ParseSubscription 解析订阅内容
func ParseSubscription(raw string) ([]Node, error) {
	var nodes []Node

	// 第一次 Base64 解码
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("第一次 Base64 解码失败: %v", err)
	}

	lines := strings.Split(string(decoded), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "vmess://"):
			n, err := parseVMess(line)
			if err == nil {
				nodes = append(nodes, n)
			}

		case strings.HasPrefix(line, "vless://"):
			n, err := parseVLESS(line)
			if err == nil {
				nodes = append(nodes, n)
			}
		}
	}
	return nodes, nil
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
