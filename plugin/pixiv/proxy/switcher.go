package proxy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/model"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FloatTech/ZeroBot-Plugin/plugin/pixiv/cache"
	"github.com/jinzhu/gorm"
)

const (
	currentIndexFile = "/root/v2ray/current_index.txt"
	configFile       = "/usr/local/etc/v2ray/config.json"
)

type Manager struct {
	db *cache.DB
}

func NewManager(db *cache.DB) *Manager {
	return &Manager{db: db}
}

// ListNodes 按插入顺序返回节点列表
func (m *Manager) ListNodes() ([]model.Node, error) {
	var nodes []model.Node
	if err := m.db.Order("record_id").Find(&nodes).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("代理节点列表为空")
		}
		return nil, errors.Join(errors.New("读取代理节点失败 "), err)
	}

	if len(nodes) == 0 {
		return nil, errors.New("代理节点列表为空")
	}

	return nodes, nil
}

// ListNodesWithDelay 返回带测速结果的节点列表（不切换）
func (m *Manager) ListNodesWithDelay(timeout time.Duration) ([]model.Node, error) {
	nodes, err := m.ListNodes()
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	for i := range nodes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			delay, err := testNode(nodes[idx], timeout)
			if err != nil {
				nodes[idx].DelayMs = -1
				return
			}
			nodes[idx].DelayMs = delay
		}(i)
	}
	wg.Wait()

	return nodes, nil
}

// SwitchTo 手动切换到指定编号的节点（从 1 开始）
func (m *Manager) SwitchTo(index int) (string, error) {
	nodes, err := m.ListNodes()
	if err != nil {
		return "", err
	}

	if index < 1 || index > len(nodes) {
		return "", fmt.Errorf("无效编号，范围 1-%d", len(nodes))
	}

	chosen := nodes[index-1]
	if err := m.writeConfigAndRestart(chosen); err != nil {
		return "", errors.Join(errors.New("切换节点失败 "), err)
	}

	return fmt.Sprintf("已切换到 #%d %s", index, chosen.Name), nil
}

// AutoSwitch 并发自动测试并切换
func (m *Manager) AutoSwitch() (string, error) {
	nodes, err := m.ListNodes()
	if err != nil {
		return "", err
	}

	total := len(nodes)
	var okCount, failCount int32

	log.Printf("开始检测 %d 个节点...\n", len(nodes))

	timeout := 4 * time.Second
	results := make(chan model.Node, len(nodes))
	var wg sync.WaitGroup

	var (
		msg     strings.Builder
		msgLock sync.Mutex
	)
	for _, n := range nodes {
		wg.Add(1)
		go func(nd model.Node) {
			defer wg.Done()
			delay, err := testNode(nd, timeout)
			if err != nil {
				msgLock.Lock()
				msg.WriteString(fmt.Sprintf("❌ %s 不可用: %v\n", nd.Name, err))
				msgLock.Unlock()
				atomic.AddInt32(&failCount, 1)
				return
			}
			nd.DelayMs = delay
			msgLock.Lock()
			msg.WriteString(fmt.Sprintf("✅ %s 可用，延迟 %.1fms\n", nd.Name, nd.DelayMs))
			msgLock.Unlock()
			atomic.AddInt32(&okCount, 1)
			results <- nd
		}(n)
	}

	// close when done
	go func() {
		wg.Wait()
		close(results)
	}()

	// pick best (smallest delay)
	var best model.Node
	best.DelayMs = 1e9
	for r := range results {
		if r.DelayMs < best.DelayMs {
			best = r
		}
	}

	if best.Name == "" {
		return "", errors.New("未发现可用节点")
	}
	msg.WriteString(fmt.Sprintf("\n检测完成：共 %d 个节点，可用 %d 个，不可用 %d 个。\n", total, okCount, failCount))
	msg.WriteString(fmt.Sprintf("\n⚡ 最佳节点: %s, 延迟 %.1fms\n", best.Name, best.DelayMs))

	// 写配置并重启 v2ray（确保重启）
	if err := m.writeConfigAndRestart(best); err != nil {
		return "", errors.Join(errors.New("切换到最佳节点失败 "), err)
	}

	return msg.String(), nil
}

func (m *Manager) writeConfigAndRestart(node model.Node) error {
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

// parseSubscription 解析订阅内容
func parseSubscription(raw string) ([]model.Node, error) {
	var nodes []model.Node

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
