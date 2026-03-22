package matching

import (
	"fmt"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"strconv"
	"time"
)

func handleCloseForwardChat(uid int64) (msg string, peerID int64, peerMsg string, err error) {
	peerID, ok := forwardManager.Close(uid)
	if !ok {
		err = fmt.Errorf("当前没有进行中的转发聊天")
		return
	}
	msg = "已关闭2小时转发聊天"
	peerMsg = "对方已主动关闭2小时转发聊天"
	return
}

func handleExtendForwardChat(uid int64, minutesText string) (msg string, peerID int64, peerMsg string, err error) {
	minutes, err := strconv.Atoi(minutesText)
	if err != nil {
		return "", 0, "", err
	}
	if minutes <= 0 {
		return "", 0, "", fmt.Errorf("请输入大于 0 的分钟数")
	}
	if minutes > 120 {
		return "", 0, "", fmt.Errorf("最多只能延长 120 分钟")
	}

	expiresAt := time.Time{}
	ok := false
	peerID, expiresAt, ok = forwardManager.Extend(uid, time.Duration(minutes)*time.Minute)
	if !ok {
		return "", 0, "", fmt.Errorf("当前没有进行中的转发聊天")
	}

	remainingSeconds := int(expiresAt.Sub(time.Now()).Seconds())
	if remainingSeconds < 0 {
		remainingSeconds = 0
	}
	remainingMinutes := (remainingSeconds + 59) / 60

	msg = fmt.Sprintf("已增加 %d 分钟转发时长，当前会话约 %d 分钟后结束", minutes, remainingMinutes)
	peerMsg = fmt.Sprintf("对方已增加 %d 分钟转发时长，当前会话约 %d 分钟后结束", minutes, remainingMinutes)
	return
}

func handleForwardMessage(ctx *zero.Ctx) {
	peerID, ok := forwardManager.GetPeer(ctx.Event.UserID)
	if !ok {
		if forwardManager.ConsumeExpiredNotice(ctx.Event.UserID) {
			ctx.SendChain(message.At(ctx.Event.UserID), message.Text("转发聊天已结束了哦"))
		}
		return
	}
	forwardMsg := message.Message{message.Text(fmt.Sprintf("来自%s[%d]的转发消息:\n", ctx.CardOrNickName(ctx.Event.UserID), ctx.Event.UserID))}
	forwardMsg = append(forwardMsg, ctx.Event.Message...)
	ctx.SendPrivateMessage(peerID, forwardMsg)
}
