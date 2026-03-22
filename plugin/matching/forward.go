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

const defaultForwardDuration = 2 * time.Hour

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

type matchSuccessEvent struct {
	ctx           *zero.Ctx
	userID        int64
	wsMsg         string
	matchedUserID int64
	matchID       string
}

type ForwardManager struct {
	mu              sync.RWMutex
	sessions        map[int64]forwardSession
	expiredNotices  map[int64]struct{}
	once            sync.Once
	matchWorkerOnce sync.Once
	matchEventCh    chan matchSuccessEvent
}

var forwardManager = newForwardManager()

func newForwardManager() *ForwardManager {
	manager := &ForwardManager{
		sessions:       map[int64]forwardSession{},
		expiredNotices: map[int64]struct{}{},
		matchEventCh:   make(chan matchSuccessEvent, 64),
	}
	manager.StartExpiryWorker()
	manager.startMatchSuccessWorker()
	return manager
}

func (m *ForwardManager) StartExpiryWorker() {
	m.once.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				expiredPairs := m.popExpiredPairs()
				m.mu.Lock()
				for _, pair := range expiredPairs {
					m.expiredNotices[pair[0]] = struct{}{}
					m.expiredNotices[pair[1]] = struct{}{}
				}
				m.mu.Unlock()
			}
		}()
	})
}

func (m *ForwardManager) Open(uid, peerID int64, duration time.Duration) bool {
	expiresAt := time.Now().Add(duration)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasActiveSessionLocked(uid) || m.hasActiveSessionLocked(peerID) {
		return false
	}
	m.sessions[uid] = forwardSession{PeerID: peerID, ExpiresAt: expiresAt}
	m.sessions[peerID] = forwardSession{PeerID: uid, ExpiresAt: expiresAt}
	delete(m.expiredNotices, uid)
	delete(m.expiredNotices, peerID)
	return true
}

func (m *ForwardManager) Close(uid int64) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[uid]
	if !ok {
		return 0, false
	}
	m.deleteSessionPairLocked(uid, session.PeerID)
	return session.PeerID, true
}

func (m *ForwardManager) GetPeer(uid int64) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getPeerLocked(uid)
}

func (m *ForwardManager) Extend(uid int64, duration time.Duration) (int64, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if duration <= 0 {
		return 0, time.Time{}, false
	}

	now := time.Now()
	session, ok := m.sessions[uid]
	if !ok {
		return 0, time.Time{}, false
	}
	if now.After(session.ExpiresAt) {
		m.deleteSessionPairLocked(uid, session.PeerID)
		return 0, time.Time{}, false
	}

	peerSession, exists := m.sessions[session.PeerID]
	if !exists || peerSession.PeerID != uid || now.After(peerSession.ExpiresAt) {
		m.deleteSessionPairLocked(uid, session.PeerID)
		return 0, time.Time{}, false
	}

	newExpiresAt := session.ExpiresAt.Add(duration)
	m.sessions[uid] = forwardSession{PeerID: session.PeerID, ExpiresAt: newExpiresAt}
	m.sessions[session.PeerID] = forwardSession{PeerID: uid, ExpiresAt: newExpiresAt}
	return session.PeerID, newExpiresAt, true
}

func (m *ForwardManager) ConsumeExpiredNotice(uid int64) bool {
	if uid == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.expiredNotices[uid]; !ok {
		return false
	}
	delete(m.expiredNotices, uid)
	return true
}

func (m *ForwardManager) HandleMatchSuccess(ev matchSuccessEvent) {
	if ev.matchedUserID == 0 {
		return
	}

	var msg strings.Builder
	msg.WriteString("匹配成功\n当前状态:")
	defer func() {
		msg.WriteString("\n如想知道我的所有功能可发送 `/用法matching`")
		msg.WriteString("\n延迟转发聊天可发送`延长转发聊天 [分钟]`")
		ev.ctx.SendPrivateMessage(ev.userID, msg.String())
	}()

	masterUserIsBotFriend := canForwardPrivateMessage(ev.ctx, ev.userID)
	matchedUserIsBotFriend := canForwardPrivateMessage(ev.ctx, ev.matchedUserID)

	switch {
	case masterUserIsBotFriend && matchedUserIsBotFriend:
		msg.WriteString("双方都是好友，可直接转发聊天")
	case masterUserIsBotFriend && !matchedUserIsBotFriend:
		msg.WriteString("你已是机器人好友，但对方可能不是（或好友列表未刷新），你仍可尝试转发聊天")
	case !masterUserIsBotFriend && matchedUserIsBotFriend:
		msg.WriteString("你暂不是机器人好友（或好友列表未刷新），但你仍可尝试转发聊天")
	default:
		msg.WriteString("双方都可能不是机器人好友（或好友列表未刷新），2小时转发聊天可能失败，但可先尝试")
	}

	if !masterUserIsBotFriend || !matchedUserIsBotFriend {
		msg.WriteString("\n如果您未接收到任何对手的消息 可能是转发失败 请您直接通过qq号添加对手进行聊天")
	}

	msg.WriteString("\n你发给机器人的私聊消息将全部转发给匹配成功的用户；可发送 `关闭转发聊天` 主动结束。")

	peerID, hasSession := m.GetPeer(ev.userID)
	matchedPeerID, matchedHasSession := m.GetPeer(ev.matchedUserID)
	if hasSession && matchedHasSession && peerID == ev.matchedUserID && matchedPeerID == ev.userID {
		msg.WriteString("\n已开启2小时转发聊天")
		return
	}

	_ = m.Open(ev.userID, ev.matchedUserID, defaultForwardDuration)
	if peerID, ok := m.GetPeer(ev.userID); ok && peerID == ev.matchedUserID {
		msg.WriteString("\n已开启2小时转发聊天")
	} else {
		msg.WriteString("\n开启转发聊天失败，请重新匹配")
	}
}

func (m *ForwardManager) EnqueueMatchSuccess(ev matchSuccessEvent, isMatchSuccess bool) {
	if !isMatchSuccess && !strings.Contains(ev.wsMsg, "匹配成功") {
		return
	}
	m.matchEventCh <- ev
}

func (m *ForwardManager) processMatching(ctx *zero.Ctx, userID int64) {
	if _, ok := m.GetPeer(userID); ok {
		ctx.SendChain(message.Text("你当前正在进行转发聊天，请先发送“关闭转发聊天”后再开始匹配"))
		return
	}

	inQueue, queueMsg, err := isUserInMatchingQueue(userID)
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
	conn, _, err := dl.Dial(fmt.Sprintf("ws://127.0.0.1:3000/api/matching/%d", userID), nil)
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
		m.EnqueueMatchSuccess(matchSuccessEvent{
			ctx:           ctx,
			userID:        userID,
			wsMsg:         rawMsg,
			matchedUserID: matchedUserID,
			matchID:       matchID,
		}, isMatchSuccess)
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(displayMsg))
	}
}

func (m *ForwardManager) startMatchSuccessWorker() {
	m.matchWorkerOnce.Do(func() {
		go func() {
			processedMatchID := make(map[string]time.Time)
			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case ev := <-m.matchEventCh:
					if ev.matchID == "" {
						ev.ctx.SendPrivateMessage(ev.userID, message.Text("匹配成功，但服务端未返回唯一对局ID（match_id），无法开启转发聊天，请联系管理员修复"))
						continue
					}
					eventKey := ev.matchID
					if expiredAt, ok := processedMatchID[eventKey]; ok && time.Now().Before(expiredAt) {
						continue
					}
					processedMatchID[eventKey] = time.Now().Add(defaultForwardDuration)
					m.HandleMatchSuccess(ev)
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

func (m *ForwardManager) popExpiredPairs() [][2]int64 {
	now := time.Now()
	pairs := make([][2]int64, 0, 4)
	m.mu.Lock()
	defer m.mu.Unlock()

	for uid, session := range m.sessions {
		peerSession, ok := m.sessions[session.PeerID]
		if !ok || peerSession.PeerID != uid {
			continue
		}
		if now.Before(session.ExpiresAt) && now.Before(peerSession.ExpiresAt) {
			continue
		}
		if uid < session.PeerID {
			pairs = append(pairs, [2]int64{uid, session.PeerID})
		}
		delete(m.sessions, uid)
		delete(m.sessions, session.PeerID)
	}
	return pairs
}

func (m *ForwardManager) hasActiveSessionLocked(uid int64) bool {
	session, ok := m.sessions[uid]
	if !ok {
		return false
	}
	if time.Now().After(session.ExpiresAt) {
		m.deleteSessionPairLocked(uid, session.PeerID)
		return false
	}
	return true
}

func (m *ForwardManager) getPeerLocked(uid int64) (int64, bool) {
	session, ok := m.sessions[uid]
	if !ok {
		return 0, false
	}
	if time.Now().After(session.ExpiresAt) {
		m.deleteSessionPairLocked(uid, session.PeerID)
		return 0, false
	}
	return session.PeerID, true
}

func (m *ForwardManager) deleteSessionPairLocked(uid, peerID int64) {
	delete(m.sessions, uid)
	if peerSession, exists := m.sessions[peerID]; exists && peerSession.PeerID == uid {
		delete(m.sessions, peerID)
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
