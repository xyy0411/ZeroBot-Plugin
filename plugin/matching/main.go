package matching

import (
	"encoding/json"
	"errors"
	"fmt"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/jinzhu/gorm"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const matchingAPIBase = "http://127.0.0.1:3000/api/matching"

var (
	helpFilePath = "./plugin/matching/help_info.txt"
	engine       = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault:  false,
		Brief:             "匹配",
		Help:              readHelpInfo(),
		PrivateDataFolder: "matching",
	})
)

var regexpstring = `^(有无|有人|谁来)(联机|匹配|打架|对决|玩吗|to|qd|lh|uu|主机|副机|主副皆可|仅主|仅副)?$`

var (
	forwardSessionMu sync.RWMutex
	forwardSessions  = map[int64]forwardSession{}
)

type matchWSPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	PeerID  int64  `json:"peer_id"`
	MatchID string `json:"match_id"`
}

type forwardSession struct {
	PeerID    int64
	ExpiresAt time.Time
}

const defaultForwardDuration = 15 * time.Minute

func init() {
	startMatchSuccessWorker()

	engine.OnFullMatch("退出被动匹配黑名单", getDB, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			err := db.Where("user_id = ?", uid).Delete(&RejectedMatchUser{}).Error
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text("已退出被动匹配黑名单"))
		})

	engine.OnRegex(regexpstring, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID

			resp, err := http.Get(apiURL("/status/" + strconv.FormatInt(uid, 10)))
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return
				}
			}

			err = db.Where("user_id = ?", uid).First(&RejectedMatchUser{}).Error
			if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
				return
			}

			ctx.SendChain(message.Text("要我帮你找人联机吗?[是|否]"))
			recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.RegexRule(`(是|否|td|退出)$`)).Repeat()
			defer cancel()

			for {
				select {
				case <-time.After(2 * time.Minute):
					return
				case r := <-recv:
					if r.Event.Message.String() != "是" {
						if err1 := db.Save(&RejectedMatchUser{UserID: uid}).Error; err1 != nil {
							ctx.SendChain(message.Text("ERROR:", err1))
							return
						}
						ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("好哦，以后就不打扰你啦"))
						return
					}
					_, err1 := doMatchInfo(uid)
					if err1 != nil {
						if err1 = ensureProfile(uid, ctx.CardOrNickName(uid), 2400); err1 != nil {
							ctx.SendChain(message.Text("ERROR:", err1))
							return
						}
					}
					processMatching(ctx, uid)
					return
				}
			}
		})

	engine.OnFullMatchGroup([]string{"查看匹配状态", "查看我的匹配状态"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			_, body, err := doRequest(http.MethodGet, "/status/"+strconv.FormatInt(uid, 10), nil, "")
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text(strings.TrimSpace(string(body))))
		})

	engine.OnFullMatch("查看匹配时间", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			m, err := doMatchInfo(uid)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text("匹配时间为 ", m.ExpireAt/60, " 分钟"))
		})

	engine.OnRegex(`删除匹配软件\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			softwareName := strings.ToLower(strings.TrimSpace(ctx.State["regex_matched"].([]string)[1]))
			path := fmt.Sprintf("/profile/%d/software/%s", uid, url.PathEscape(softwareName))
			status, body, err := doRequest(http.MethodDelete, path, nil, "")
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			if status >= 200 && status < 300 {
				ctx.SendChain(message.Text(fmt.Sprintf("%s[%d] 删除匹配软件成功", ctx.CardOrNickName(uid), uid)))
				return
			}
			m := make(map[string]string)
			json.Unmarshal(body, &m)
			ctx.SendChain(message.Text(m["message"]))
		})

	engine.OnRegex(`删除匹配黑名单\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			targetText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			targetID, err := strconv.ParseInt(targetText, 10, 64)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			path := fmt.Sprintf("/profile/%d/block-user/%d", uid, targetID)
			status, body, err := doRequest(http.MethodDelete, path, nil, "")
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			if status >= 200 && status < 300 {
				ctx.SendChain(message.Text(fmt.Sprintf("%s[%d] 删除黑名单成功", ctx.CardOrNickName(uid), uid)))
				return
			}
			m := make(map[string]string)
			json.Unmarshal(body, &m)

			ctx.SendChain(message.Text(m["message"]))
		})

	engine.OnFullMatch("查看匹配黑名单列表", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			m, err := doMatchInfo(uid)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			blockUsers := m.BlockUsers
			var msg strings.Builder
			msg.WriteString("黑名单列表:\n")
			for i, blockUser := range blockUsers {
				msg.WriteString("第")
				msg.WriteString(strconv.Itoa(i + 1))
				msg.WriteString("个:")
				msg.WriteString(strconv.FormatInt(blockUser.UserID, 10) + "\n")
			}
			ctx.SendChain(message.Text(msg.String()))
		})

	engine.OnFullMatchGroup([]string{"查看匹配软件", "查看我的匹配软件"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			m, err := doMatchInfo(uid)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			SoftwareList := m.OnlineSoftwares
			var msg strings.Builder
			msg.WriteString("当前可用匹配软件如下:\n")
			if len(SoftwareList) == 0 {
				msg.WriteString("暂未设置任何匹配软件")
				ctx.SendChain(message.Text(msg.String()))
				return
			}

			for i, software := range SoftwareList {
				var softwareType string
				switch software.Type {
				case 0:
					softwareType = "主副皆可"
				case 1:
					softwareType = "仅主"
				case 2:
					softwareType = "仅副"
				default:
					softwareType = fmt.Sprintf("未知类型(%d)", software.Type)
				}
				msg.WriteString("第")
				msg.WriteString(strconv.Itoa(i + 1))
				msg.WriteString("个 ")
				msg.WriteString(software.Name + ": " + softwareType + "\n")
			}
			ctx.SendChain(message.Text(msg.String()))
		})

	engine.OnRegex(`设置匹配黑名单\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			targetText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			targetID, err := strconv.ParseInt(targetText, 10, 64)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}

			if err = ensureProfile(uid, ctx.CardOrNickName(uid), 2000); err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			if err = addBlockUser(uid, targetID); err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d] 添加黑名单成功", ctx.CardOrNickName(uid), uid)))
		})

	engine.OnRegex(`设置匹配时间\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			minutesText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			minutes, err := strconv.Atoi(minutesText)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			seconds := int64(minutes * 60)

			if err = ensureProfile(uid, ctx.CardOrNickName(uid), seconds); err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			if err = updateExpire(uid, seconds); err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Text(fmt.Sprintf("%s[%d] 设置匹配时间成功", ctx.CardOrNickName(uid), uid)))
		})

	engine.OnRegex(`设置匹配软件\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			software := strings.ToLower(strings.ReplaceAll(ctx.State["regex_matched"].([]string)[1], " ", ""))

			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("请输入软件类型 [主副皆可|仅主|仅副]"))
			recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.RegexRule(`^(.+)$`)).Repeat()
			defer cancel()

			timer := time.NewTimer(120 * time.Second)
			defer timer.Stop()

			for {
				select {
				case <-timer.C:
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("超时未输入"))
					return
				case r := <-recv:
					answer := strings.TrimSpace(r.Event.Message.String())
					softwareType, ok := parseSoftwareType(answer)
					if !ok {
						ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("输入错误，请输入 主副皆可/仅主/仅副"))
						continue
					}

					if err := ensureProfile(uid, ctx.CardOrNickName(uid), 1800); err != nil {
						ctx.SendChain(message.Text("ERROR:", err))
						return
					}
					if err := addSoftware(uid, software, softwareType); err != nil {
						ctx.SendChain(message.Text("ERROR:", err))
						return
					}
					ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(fmt.Sprintf("%s[%d] 设置匹配软件成功", ctx.CardOrNickName(uid), uid)))
					return
				}
			}
		})

	engine.OnFullMatchGroup([]string{"取消匹配", "退出匹配"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			_, _, err := doRequest(http.MethodDelete, "/"+strconv.FormatInt(uid, 10), nil, "")
			if err != nil {
				ctx.SendChain(message.Text(err))
				return
			}
		})

	engine.OnFullMatchGroup([]string{"开始匹配", "匹配", "匹配开始"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			if err := ensureProfile(uid, ctx.CardOrNickName(uid), 1800); err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			processMatching(ctx, uid)
		})

	engine.OnFullMatchGroup([]string{"关闭转发聊天", "结束转发聊天"}).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			peerID, ok := unregisterForwardSession(ctx.Event.UserID)
			if !ok {
				ctx.SendChain(message.Text("当前没有进行中的转发聊天"))
				return
			}
			ctx.SendChain(message.Text("已关闭15分钟转发聊天"))
			ctx.SendPrivateMessage(peerID, message.Text("对方已主动关闭15分钟转发聊天"))
		})

	engine.OnRegex("^(?:增加转发时长|延长转发聊天)\\s*(\\d+)$", zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			uid := ctx.Event.UserID
			minutesText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			minutes, err := strconv.Atoi(minutesText)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			if minutes <= 0 {
				ctx.SendChain(message.Text("请输入大于 0 的分钟数"))
				return
			}

			peerID, expiresAt, ok := extendForwardSession(uid, time.Duration(minutes)*time.Minute)
			if !ok {
				ctx.SendChain(message.Text("当前没有进行中的转发聊天"))
				return
			}

			remainingSeconds := int(expiresAt.Sub(time.Now()).Seconds())
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
			remainingMinutes := (remainingSeconds + 59) / 60

			ctx.SendChain(message.Text(fmt.Sprintf("已增加 %d 分钟转发时长，当前会话约 %d 分钟后结束", minutes, remainingMinutes)))
			ctx.SendPrivateMessage(peerID, message.Text(fmt.Sprintf("对方已增加 %d 分钟转发时长，当前会话约 %d 分钟后结束", minutes, remainingMinutes)))
		})

	engine.OnMessage(zero.OnlyPrivate, zero.OnlyToMe).SetBlock(false).
		Handle(func(ctx *zero.Ctx) {
			peerID, ok := getForwardPeer(ctx.Event.UserID)
			if !ok {
				return
			}
			forwardMsg := message.Message{message.Text(fmt.Sprintf("来自%s[%d]的转发消息:\n", ctx.CardOrNickName(ctx.Event.UserID), ctx.Event.UserID))}
			forwardMsg = append(forwardMsg, ctx.Event.Message...)
			ctx.SendPrivateMessage(peerID, forwardMsg)
		})
}

func readHelpInfo() string {
	content, err := os.ReadFile(helpFilePath)
	if err != nil {
		fmt.Printf("读取帮助信息文件失败: %v\n", err)
		return ""
	}
	return string(content)
}

func isUserInMatchingQueue(uid int64) (bool, string, error) {
	status, body, err := doRequest(http.MethodGet, "/status/"+strconv.FormatInt(uid, 10), nil, "")
	if err != nil {
		return false, "", err
	}
	return status == http.StatusOK, strings.TrimSpace(string(body)), nil
}

func parseSoftwareType(input string) (int8, bool) {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "主副皆可", "主副":
		return 0, true
	case "仅主", "主":
		return 1, true
	case "仅副", "副":
		return 2, true
	default:
		return 0, false
	}
}
