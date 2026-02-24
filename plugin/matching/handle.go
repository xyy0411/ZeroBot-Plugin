package matching

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
	"io"
	"strings"
	"sync"
	"time"
)

type matchSuccessEvent struct {
	ctx           *zero.Ctx
	userID        int64
	wsMsg         string
	matchedUserID int64
	matchID       string
}

var (
	matchSuccessWorkerOnce sync.Once
	matchSuccessEventCh    = make(chan matchSuccessEvent, 64)
)

func startMatchSuccessWorker() {
	matchSuccessWorkerOnce.Do(func() {
		go func() {
			processedMatchID := make(map[string]time.Time)
			ticker := time.NewTicker(10 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case ev := <-matchSuccessEventCh:
					if ev.matchID == "" {
						ev.ctx.SendPrivateMessage(ev.userID, message.Text("匹配成功，但服务端未返回唯一对局ID（match_id），无法开启转发聊天，请联系后端修复。"))
						continue
					}
					eventKey := fmt.Sprintf("%s:%d", ev.matchID, ev.userID)
					if expiredAt, ok := processedMatchID[eventKey]; ok && time.Now().Before(expiredAt) {
						continue
					}
					processedMatchID[eventKey] = time.Now().Add(defaultForwardDuration)
					handleMatchSuccessEvent(ev)
				case <-ticker.C:
					now := time.Now()
					for matchID, expiredAt := range processedMatchID {
						if now.After(expiredAt) {
							delete(processedMatchID, matchID)
						}
					}
				}
			}
		}()
	})
}

func handleMatchSuccessEvent(ev matchSuccessEvent) {
	if ev.matchedUserID == 0 {
		return
	}
	if !canForwardPrivateMessage(ev.ctx, ev.userID) {
		notice := message.Text("匹配成功，但你当前还不是机器人好友（或好友列表未刷新），暂时无法开启15分钟转发聊天。")
		ev.ctx.SendPrivateMessage(ev.userID, notice)
		return
	}
	if !registerForwardSession(ev.userID, ev.matchedUserID, defaultForwardDuration) {
		if peerID, ok := getForwardPeer(ev.userID); ok && peerID == ev.matchedUserID {
			notice := message.Text("匹配成功，已开启15分钟转发聊天。你发送给机器人的私聊消息将全部转发给匹配成功的用户；可发送“关闭转发聊天”主动结束。如想知道我的所有功能可发送 `/用法matching`")
			ev.ctx.SendPrivateMessage(ev.userID, notice)
		}
		return
	}
	notice := message.Text("匹配成功，已开启15分钟转发聊天。你发送给机器人的私聊消息将全部转发给匹配成功的用户；可发送“关闭转发聊天”主动结束。如想知道我的所有功能可发送 `/用法matching`")
	ev.ctx.SendPrivateMessage(ev.userID, notice)
}

func enqueueMatchSuccessNotice(ctx *zero.Ctx, userID int64, wsMsg string, matchedUserID int64, matchID string, isMatchSuccess bool) {
	if !isMatchSuccess && !strings.Contains(wsMsg, "匹配成功") {
		return
	}
	matchSuccessEventCh <- matchSuccessEvent{
		ctx:           ctx,
		userID:        userID,
		wsMsg:         wsMsg,
		matchedUserID: matchedUserID,
		matchID:       matchID,
	}
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
		displayMsg, matchedUserID, matchID, isMatchSuccess := parseMatchWSMessage(msg)
		enqueueMatchSuccessNotice(ctx, user.UserID, rawMsg, matchedUserID, matchID, isMatchSuccess)
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(displayMsg))
	}
}

func parseMatchWSMessage(raw []byte) (displayMsg string, matchedUserID int64, matchID string, isMatchSuccess bool) {
	rawText := string(raw)
	displayMsg = rawText

	var payload matchWSPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return displayMsg, 0, "", strings.Contains(rawText, "匹配成功")
	}
	if payload.Message != "" {
		displayMsg = payload.Message
	}
	if payload.PeerID != 0 {
		matchedUserID = payload.PeerID
	}
	matchID = payload.MatchID
	if payload.Type == "match_success" {
		isMatchSuccess = true
	}
	if !isMatchSuccess && strings.Contains(displayMsg, "匹配成功") {
		isMatchSuccess = true
	}
	return displayMsg, matchedUserID, matchID, isMatchSuccess
}

func canForwardPrivateMessage(ctx *zero.Ctx, matchedID int64) bool {
	if matchedID == 0 {
		return false
	}
	friendIDs := getFriendIDSet(ctx)
	_, ok := friendIDs[matchedID]
	return ok
}

func getFriendIDSet(ctx *zero.Ctx) map[int64]struct{} {
	friendIDs := make(map[int64]struct{})
	for _, friend := range ctx.GetFriendList().Array() {
		id := friend.Get("user_id").Int()
		if id != 0 {
			friendIDs[id] = struct{}{}
		}
	}
	return friendIDs
}

func registerForwardSession(uid, peerID int64, duration time.Duration) bool {
	expiresAt := time.Now().Add(duration)
	forwardSessionMu.Lock()
	defer forwardSessionMu.Unlock()
	if hasActiveForwardSessionLocked(uid) || hasActiveForwardSessionLocked(peerID) {
		return false
	}
	forwardSessions[uid] = forwardSession{PeerID: peerID, ExpiresAt: expiresAt}
	forwardSessions[peerID] = forwardSession{PeerID: uid, ExpiresAt: expiresAt}
	return true
}

func hasActiveForwardSessionLocked(uid int64) bool {
	session, ok := forwardSessions[uid]
	if !ok {
		return false
	}
	if time.Now().After(session.ExpiresAt) {
		delete(forwardSessions, uid)
		if peerSession, exists := forwardSessions[session.PeerID]; exists && peerSession.PeerID == uid {
			delete(forwardSessions, session.PeerID)
		}
		return false
	}
	return true
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
