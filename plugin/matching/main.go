package matching

import (
	"fmt"
	"os"
	"strings"

	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
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

func init() {
	engine.OnFullMatch("更新个人信息", zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleUpdateProfile(ctx.Event.UserID, ctx.CardOrNickName(ctx.Event.UserID))
			sendTextResult(ctx, msg, err)
		})
	engine.OnFullMatch("退出被动匹配黑名单", getDB, zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleRemoveRejectedMatchUser(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(regexpstring, getDB).SetBlock(true).
		Handle(handlePassiveMatchingPrompt)

	engine.OnFullMatchGroup([]string{"查看匹配状态", "查看我的匹配状态"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleViewMatchingStatus(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnFullMatch("查看匹配时间", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleViewMatchingExpire(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(`删除匹配软件\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			softwareName := strings.ToLower(strings.TrimSpace(ctx.State["regex_matched"].([]string)[1]))
			msg, err := handleDeleteSoftware(ctx.Event.UserID, softwareName)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(`删除匹配黑名单\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			targetText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			msg, err := handleDeleteBlockUser(ctx.Event.UserID, targetText)
			sendTextResult(ctx, msg, err)
		})

	engine.OnFullMatch("查看匹配黑名单列表", getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleViewBlockUsers(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnFullMatchGroup([]string{"查看匹配软件", "查看我的匹配软件"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleViewSoftware(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(`设置匹配黑名单\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			targetText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			msg, err := handleSetBlockUser(ctx.Event.UserID, ctx.CardOrNickName(ctx.Event.UserID), targetText)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(`设置匹配时间\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			minutesText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			msg, err := handleSetMatchingExpire(ctx.Event.UserID, ctx.CardOrNickName(ctx.Event.UserID), minutesText)
			sendTextResult(ctx, msg, err)
		})

	engine.OnRegex(`设置匹配软件\s*(.+)`, zero.OnlyPrivate, getDB).SetBlock(true).
		Handle(handleSetSoftware)

	engine.OnFullMatchGroup([]string{"取消匹配", "退出匹配", "停止匹配"}, getDB).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, err := handleCancelMatching(ctx.Event.UserID)
			sendTextResult(ctx, msg, err)
		})

	engine.OnFullMatchGroup([]string{"开始匹配", "匹配", "匹配开始"}, getDB).SetBlock(true).
		Handle(handleStartMatching)

	engine.OnFullMatchGroup([]string{"关闭转发聊天", "结束转发聊天"}).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			msg, peerID, peerMsg, err := handleCloseForwardChat(ctx.Event.UserID)
			if err != nil {
				sendTextResult(ctx, "", err)
				return
			}
			ctx.SendChain(message.Text(msg))
			if peerID != 0 && peerMsg != "" {
				ctx.SendPrivateMessage(peerID, message.Text(peerMsg))
			}
		})

	engine.OnRegex("^(?:增加转发时长|延长转发聊天)\\s*(\\d+)$", zero.OnlyPrivate).SetBlock(true).
		Handle(func(ctx *zero.Ctx) {
			minutesText := strings.TrimSpace(ctx.State["regex_matched"].([]string)[1])
			msg, peerID, peerMsg, err := handleExtendForwardChat(ctx.Event.UserID, minutesText)
			if err != nil {
				sendTextResult(ctx, "", err)
				return
			}
			ctx.SendChain(message.Text(msg))
			if peerID != 0 && peerMsg != "" {
				ctx.SendPrivateMessage(peerID, message.Text(peerMsg))
			}
		})

	engine.OnMessage(zero.OnlyPrivate, zero.OnlyToMe).SetBlock(false).
		Handle(handleForwardMessage)
}

func readHelpInfo() string {
	content, err := os.ReadFile(helpFilePath)
	if err != nil {
		fmt.Printf("读取帮助信息文件失败: %v\n", err)
		return ""
	}
	return string(content)
}

func sendTextResult(ctx *zero.Ctx, msg string, err error) {
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	if msg != "" {
		ctx.SendChain(message.Text(msg))
	}
}
