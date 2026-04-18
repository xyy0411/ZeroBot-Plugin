package matching

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func handleUpdateProfile(userID int64, nickname string) (string, error) {
	return updateName(userID, nickname)
}

func handleDeleteSoftware(uid int64, softwareName string) (string, error) {
	return deleteSoftware(uid, softwareName)
}

func handleDeleteBlockUser(uid int64, targetText string) (string, error) {
	targetID, err := strconv.ParseInt(targetText, 10, 64)
	if err != nil {
		return "", err
	}
	msg, err := deleteBlockUser(uid, targetID)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return fmt.Sprintf("已删除黑名单用户 %d", targetID), nil
	}
	return msg, nil
}

func handleViewBlockUsers(uid int64) (string, error) {
	m, err := doMatchInfo(uid)
	if err != nil {
		return "", err
	}
	if len(m.BlockUsers) == 0 {
		return "黑名单列表为空", nil
	}

	var msg strings.Builder
	msg.WriteString("黑名单列表:\n")
	for i, blockUser := range m.BlockUsers {
		msg.WriteString("第")
		msg.WriteString(strconv.Itoa(i + 1))
		msg.WriteString("个:")
		msg.WriteString(strconv.FormatInt(blockUser.UserID, 10))
		msg.WriteString("\n")
	}
	return msg.String(), nil
}

func handleViewSoftware(uid int64) (string, error) {
	m, err := doMatchInfo(uid)
	if err != nil {
		return "", err
	}

	var msg strings.Builder
	msg.WriteString("当前可用匹配软件如下:\n")
	if len(m.OnlineSoftwares) == 0 {
		msg.WriteString("暂未设置任何匹配软件")
		return msg.String(), nil
	}

	for i, software := range m.OnlineSoftwares {
		msg.WriteString("第")
		msg.WriteString(strconv.Itoa(i + 1))
		msg.WriteString("个 ")
		msg.WriteString(software.Name)
		msg.WriteString(": ")
		msg.WriteString(softwareTypeName(software.Type))
		msg.WriteString("\n")
	}
	return msg.String(), nil
}

func handleSetBlockUser(uid int64, nickname, targetText string) (string, error) {
	if len(targetText) >= 15 {
		return "", fmt.Errorf("用户ID不合法")
	}
	targetID, err := strconv.ParseInt(targetText, 10, 64)
	if err != nil {
		return "", err
	}

	if err = ensureProfile(uid, nickname, defaultProfileExpireBlock); err != nil {
		return "", err
	}
	msg, err := addBlockUser(uid, targetID)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return fmt.Sprintf("%s[%d] 添加黑名单成功", nickname, uid), nil
	}
	return msg, nil
}

func handleSetMatchingExpire(uid int64, nickname, minutesText string) (string, error) {
	minutes, err := strconv.Atoi(minutesText)
	if err != nil {
		return "", err
	}
	seconds := int64(minutes * 60)

	if err = ensureProfile(uid, nickname, seconds); err != nil {
		return "", err
	}
	msg, err := updateExpire(uid, seconds)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return fmt.Sprintf("%s[%d] 设置匹配时间成功", nickname, uid), nil
	}
	return msg, nil
}

func handleSetSoftware(ctx *zero.Ctx) {
	uid := ctx.Event.UserID
	nickname := ctx.CardOrNickName(uid)
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

			msg, err := setSoftwareWithType(uid, nickname, software, softwareType)
			if err != nil {
				ctx.SendChain(message.Text("ERROR:", err))
				return
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
			return
		}
	}
}

func setSoftwareWithType(uid int64, nickname, software string, softwareType int8) (string, error) {
	if err := ensureProfile(uid, nickname, defaultProfileExpire); err != nil {
		return "", err
	}
	msg, err := addSoftware(uid, software, softwareType)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return fmt.Sprintf("%s[%d] 设置匹配软件成功", nickname, uid), nil
	}
	return msg, nil
}
