package matching

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"io"
	"strconv"
	"strings"
	"time"
)

func processMatchSuccessNotice(ctx *zero.Ctx, userID int64, wsMsg string, matchedUserID int64, isMatchSuccess bool) {
	if !isMatchSuccess && !strings.Contains(wsMsg, "匹配成功") {
		return
	}
	if _, ok := getForwardPeer(userID); ok {
		return
	}
	if matchedUserID == 0 {
		matchedUserID = parseMatchedIDFromText(ctx, userID, wsMsg)
	}
	if matchedUserID == 0 {
		return
	}
	/*	if !isBotFriend(ctx, userID, matchedUserID) {
		notice := message.Text("匹配成功，但双方必须都先加机器人好友，才能开启15分钟转发聊天。")
		ctx.SendPrivateMessage(userID, notice)
		return
	}*/
	registerForwardSession(userID, matchedUserID, defaultForwardDuration)
	notice := message.Text("匹配成功，已开启15分钟转发聊天。你发送给机器人的私聊消息将全部转发给匹配成功的用户；可发送“关闭转发聊天”主动结束。如想知道我的所有功能可发送 `/用法matching`")
	ctx.SendPrivateMessage(userID, notice)
}

func processMatching(ctx *zero.Ctx, user User) {
	if _, ok := getForwardPeer(user.UserID); ok {
		ctx.SendChain(message.Text("你当前正在进行转发聊天，请先发送“关闭转发聊天”后再开始匹配"))
		return
	}

	inQueue, queueMsg, err := isUserInMatchingQueue(user.UserID)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	if inQueue {
		if queueMsg == "" {
			queueMsg = "你已经在匹配队列中了，无需重复开始匹配"
		}
		ctx.SendChain(message.Text(queueMsg))
		return
	}

	var dl websocket.Dialer
	conn, _, err := dl.Dial(fmt.Sprintf("ws://127.0.0.1:3000/api/matching/%d", user.UserID), nil)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}

	err = conn.WriteJSON(user)
	if err != nil {
		ctx.SendChain(message.Text("ERROR:", err))
		return
	}
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) || errors.Is(err, io.EOF) {
				return
			}
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}

		rawMsg := string(msg)
		displayMsg, matchedUserID, isMatchSuccess := parseMatchWSMessage(msg)
		processMatchSuccessNotice(ctx, user.UserID, rawMsg, matchedUserID, isMatchSuccess)
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(displayMsg))
	}
}

func parseMatchWSMessage(raw []byte) (displayMsg string, matchedUserID int64, isMatchSuccess bool) {
	rawText := string(raw)
	displayMsg = rawText

	var payload matchWSPayload
	if err := json.Unmarshal(raw, &payload); err == nil {
		if payload.Message != "" {
			displayMsg = payload.Message
		}
		if payload.PeerID != 0 {
			matchedUserID = payload.PeerID
		}
		if payload.Type == "match_success" || payload.Type == "matched" {
			isMatchSuccess = true
		}
		if !isMatchSuccess && strings.Contains(displayMsg, "匹配成功") {
			isMatchSuccess = true
		}
		if isMatchSuccess {
			return displayMsg, matchedUserID, true
		}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return displayMsg, 0, strings.Contains(rawText, "匹配成功")
	}

	readString := func(key string) string {
		v, ok := obj[key]
		if !ok {
			return ""
		}
		var val string
		if err := json.Unmarshal(v, &val); err != nil {
			return ""
		}
		return val
	}
	readInt64 := func(key string) int64 {
		v, ok := obj[key]
		if !ok {
			return 0
		}
		var val int64
		if err := json.Unmarshal(v, &val); err == nil {
			return val
		}
		var strVal string
		if err := json.Unmarshal(v, &strVal); err == nil {
			if id, convErr := strconv.ParseInt(strVal, 10, 64); convErr == nil {
				return id
			}
		}
		return 0
	}

	if msg := readString("message"); msg != "" {
		displayMsg = msg
	}
	if tp := readString("type"); tp == "match_success" || tp == "matched" {
		isMatchSuccess = true
	}
	for _, key := range []string{"peer_id", "matched_user_id", "matched_id", "target_id", "peerId", "matchedUserID"} {
		if id := readInt64(key); id != 0 {
			matchedUserID = id
			break
		}
	}
	if !isMatchSuccess && strings.Contains(displayMsg, "匹配成功") {
		isMatchSuccess = true
	}
	return displayMsg, matchedUserID, isMatchSuccess
}

func isBotFriend(ctx *zero.Ctx, uid, matchedID int64) bool {
	friends := ctx.GetFriendList().Array()
	friendMap := make(map[int64]bool)

	for _, friend := range friends {
		friendMap[friend.Get("user_id").Int()] = true
	}

	return friendMap[uid] && friendMap[matchedID]
}

func registerForwardSession(uid, peerID int64, duration time.Duration) {
	expiresAt := time.Now().Add(duration)
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	forwardSessions[uid] = forwardSession{PeerID: peerID, ExpiresAt: expiresAt}
	forwardSessions[peerID] = forwardSession{PeerID: uid, ExpiresAt: expiresAt}
}

func getForwardPeer(uid int64) (int64, bool) {
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	session, ok := forwardSessions[uid]
	if !ok {
		return 0, false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(forwardSessions, uid)
		if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
			delete(forwardSessions, session.PeerID)
		}
		return 0, false
	}
	return session.PeerID, true
}

func unregisterForwardSession(uid int64) (int64, bool) {
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	session, ok := forwardSessions[uid]
	if !ok {
		return 0, false
	}
	delete(forwardSessions, uid)
	if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
		delete(forwardSessions, session.PeerID)
	}
	return session.PeerID, true
}

func parseMatchedIDFromText(ctx *zero.Ctx, uid int64, text string) int64 {
	friendSet := make(map[int64]struct{})
	for _, friend := range ctx.GetFriendList().Array() {
		friendSet[friend.Get("user_id").Int()] = struct{}{}
	}

	parseID := func(raw string) (int64, bool) {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id == uid {
			return 0, false
		}
		return id, true
	}

	for _, reg := range matchIDRegexps {
		matched := reg.FindStringSubmatch(text)
		if len(matched) < 2 {
			continue
		}
		id, ok := parseID(matched[1])
		if !ok {
			continue
		}
		if _, isFriend := friendSet[id]; isFriend {
			return id
		}
	}

	var fallback int64
	matches := qqRegexp.FindAllString(text, -1)
	for _, m := range matches {
		id, ok := parseID(m)
		if !ok {
			continue
		}
		if _, isFriend := friendSet[id]; isFriend {
			return id
		}
		if fallback == 0 {
			fallback = id
		}
	}
	return fallback
}
