package bot

import (
	"context"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ref names one rendered page. Callback payloads carry it so a page can restore
// the list filter, page, and cursor the user left behind; the bot keeps no
// navigation history of its own.
type ref struct {
	kind  string
	id    int64
	after int64
	code  string
	state listState
}

const (
	refHome     = "h"
	refSettings = "g"
	refJobs     = "j"
	refServices = "s"
	refCats     = "c"
	refService  = "d"
	refPeriod   = "p"
	refRules    = "r"
	refRule     = "u"
)

type navSession struct {
	current ref
	stack   []ref
}

// serviceList is the overview page a list state stands for.
func serviceList(state listState) ref { return ref{kind: refServices, state: state} }

func serviceCategories(state listState) ref { return ref{kind: refCats, state: state} }

// homePage is the menu itself.
func homePage() ref { return ref{kind: refHome} }

// serviceRef is one service detail page with the list it came from.
func serviceRef(serviceID int64, state listState) ref {
	return ref{kind: refService, id: serviceID, state: state}
}

// periodRef is one period page with the list behind it.
func periodRef(periodID int64, state listState) ref {
	return ref{kind: refPeriod, id: periodID, state: state}
}

// rulesRef is a period's rule list at one cursor.
func rulesRef(periodID, after int64, state listState) ref {
	return ref{kind: refRules, id: periodID, after: after, state: state}
}

// ruleRef is one rule page with the rule list cursor behind it.
func ruleRef(ruleID, after int64, state listState) ref {
	return ref{kind: refRule, id: ruleID, after: after, state: state}
}

// jobsPage is the reminder list for one filter and cursor.
func jobsPage(code string, after int64) ref { return ref{kind: refJobs, code: code, after: after} }

// settingsPage is the settings screen, whose parent is the menu.
func settingsPage() ref { return ref{kind: refSettings} }

// token encodes the ref into one callback argument. Fields are dot-separated
// inside a single part, so the payload stays far below Telegram's 64-byte limit.
func (r ref) token() string {
	switch r.kind {
	case refHome, refSettings:
		return r.kind
	case refCats:
		return r.kind + "." + r.state.token()
	case refJobs:
		return r.kind + "." + r.code + "." + base36(r.after)
	case refServices:
		return r.kind + "." + r.state.token()
	case refService, refPeriod:
		return r.kind + "." + id(r.id) + "." + r.state.token()
	case refRules, refRule:
		return r.kind + "." + id(r.id) + "." + base36(r.after) + "." + r.state.token()
	}
	return ""
}

func parseRef(token string) (ref, bool) {
	parts := strings.Split(token, ".")
	r := ref{kind: parts[0]}
	args := parts[1:]
	switch r.kind {
	case refHome, refSettings:
		return r, len(args) == 0
	case refCats:
		if len(args) != 3 {
			return ref{}, false
		}
		state, ok := parseListState(strings.Join(args, "."))
		if !ok {
			return ref{}, false
		}
		r.state = state
		return r, true
	case refJobs:
		if len(args) != 2 {
			return ref{}, false
		}
		if _, ok := jobFilterFromCode(args[0]); !ok {
			return ref{}, false
		}
		after, ok := parseBase36(args[1])
		if !ok {
			return ref{}, false
		}
		r.code, r.after = args[0], after
		return r, true
	case refServices:
		state, ok := parseListState(strings.Join(args, "."))
		if !ok {
			return ref{}, false
		}
		r.state = state
		return r, true
	case refService, refPeriod:
		if len(args) != 4 {
			return ref{}, false
		}
		target, ok := callbackID(args, 0)
		if !ok {
			return ref{}, false
		}
		state, ok := parseListState(strings.Join(args[1:], "."))
		if !ok {
			return ref{}, false
		}
		r.id, r.state = target, state
		return r, true
	case refRules, refRule:
		if len(args) != 5 {
			return ref{}, false
		}
		target, ok := callbackID(args, 0)
		if !ok {
			return ref{}, false
		}
		after, ok := parseBase36(args[1])
		if !ok {
			return ref{}, false
		}
		state, ok := parseListState(strings.Join(args[2:], "."))
		if !ok {
			return ref{}, false
		}
		r.id, r.after, r.state = target, after, state
		return r, true
	}
	return ref{}, false
}

// navRow ends a page with its return target and the menu. The menu is the help
// page itself, so a page that came from it shows only the menu button.
func navRow(back ref) []models.InlineKeyboardButton {
	if back.kind == refHome {
		return []models.InlineKeyboardButton{button("主菜单", cb("nav", "home"))}
	}
	return []models.InlineKeyboardButton{button("← 返回", cb("nav", "go", back.token())), button("主菜单", cb("nav", "home"))}
}

func (h *Handler) navSet(chatID int64, current ref) {
	h.navMu.Lock()
	defer h.navMu.Unlock()
	if h.nav == nil {
		h.nav = make(map[int64]navSession)
	}
	s := h.nav[chatID]
	s.current = current
	h.nav[chatID] = s
}

func (h *Handler) navReset(chatID int64, current ref) {
	h.navMu.Lock()
	defer h.navMu.Unlock()
	if h.nav == nil {
		h.nav = make(map[int64]navSession)
	}
	h.nav[chatID] = navSession{current: current}
}

func (h *Handler) navCurrent(chatID int64) (ref, bool) {
	h.navMu.Lock()
	defer h.navMu.Unlock()
	s, ok := h.nav[chatID]
	if !ok || s.current.kind == "" {
		return ref{}, false
	}
	return s.current, true
}

func (h *Handler) navPush(chatID int64) {
	h.navMu.Lock()
	defer h.navMu.Unlock()
	if h.nav == nil {
		h.nav = make(map[int64]navSession)
	}
	s := h.nav[chatID]
	if s.current.kind == "" {
		return
	}
	if n := len(s.stack); n == 0 || s.stack[n-1].token() != s.current.token() {
		s.stack = append(s.stack, s.current)
		if len(s.stack) > 16 {
			s.stack = s.stack[len(s.stack)-16:]
		}
	}
	h.nav[chatID] = s
}

func (h *Handler) navPushFrom(chatID int64, fallback ref) {
	if current, ok := h.navCurrent(chatID); !ok || current.kind == refHome {
		h.navSet(chatID, fallback)
	}
	h.navPush(chatID)
}

func (h *Handler) navPop(chatID int64) (ref, bool) {
	h.navMu.Lock()
	defer h.navMu.Unlock()
	s, ok := h.nav[chatID]
	if !ok || len(s.stack) == 0 {
		return ref{}, false
	}
	n := len(s.stack) - 1
	target := s.stack[n]
	s.stack = s.stack[:n]
	s.current = target
	h.nav[chatID] = s
	return target, true
}

func (h *Handler) navButtons(chatID int64, fallback ref) []models.InlineKeyboardButton {
	if current, ok := h.navCurrent(chatID); ok {
		_ = current
		h.navMu.Lock()
		s := h.nav[chatID]
		h.navMu.Unlock()
		if len(s.stack) > 0 {
			return navRow(s.stack[len(s.stack)-1])
		}
	}
	return navRow(fallback)
}

func (h *Handler) navCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	switch v3.action {
	case "home":
		h.navReset(resp.chatID, homePage())
		h.helpMessage(ctx, b, user, resp)
		return silentAnswer
	case "go":
		if len(v3.args) != 1 {
			return "未知操作"
		}
		target, ok := parseRef(v3.args[0])
		if !ok {
			return "未知操作"
		}
		if current, exists := h.navCurrent(resp.chatID); exists {
			h.navMu.Lock()
			s := h.nav[resp.chatID]
			matchesBack := len(s.stack) > 0 && s.stack[len(s.stack)-1].token() == target.token()
			h.navMu.Unlock()
			if matchesBack {
				_, _ = h.navPop(resp.chatID)
			} else if current.token() != target.token() {
				h.navSet(resp.chatID, target)
			}
		}
		h.showRef(ctx, b, user, resp, target)
		return silentAnswer
	}
	return "未知操作"
}

// showRef re-renders a page from its ref. Every back, cancel, and post-flow
// button lands here so no screen hard-codes the page it returns to.
func (h *Handler) showRef(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, r ref) {
	switch r.kind {
	case refHome:
		h.helpMessage(ctx, b, user, resp)
	case refSettings:
		h.showSettings(ctx, b, user, resp)
	case refJobs:
		filter, _ := jobFilterFromCode(r.code)
		h.showJobs(ctx, b, user, resp, filter, r.after)
	case refServices:
		h.showOverview(ctx, b, user.ID, resp, r.state.filter(), r.state.page)
	case refCats:
		h.showCategories(ctx, b, user.ID, resp, r.state)
	case refService:
		h.showServiceDetail(ctx, b, user, resp, r.id, r.state, 0)
	case refPeriod:
		h.showPeriodDetail(ctx, b, user, resp, r.id, r.state)
	case refRules:
		h.showRules(ctx, b, user, resp, r.id, r.after, r.state)
	case refRule:
		h.showRuleDetail(ctx, b, user, resp, r.id, r.after, r.state)
	default:
		h.send(ctx, b, resp, expiredButtonMessage)
	}
}
