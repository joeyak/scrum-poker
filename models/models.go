package models

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type CookieData struct {
	User    UserInfo
	Session SessionInfo
}

type SessionInfo struct {
	Cards          []string
	Rows           []string
	MapToFibonacci bool
}

func NewSessionInfo(cards, rows []string, mapToFibonacci bool) SessionInfo {
	if len(rows) == 0 {
		rows = []string{""}
	}
	return SessionInfo{
		Cards:          cards,
		Rows:           rows,
		MapToFibonacci: mapToFibonacci,
	}
}

type Session struct {
	SessionInfo
	ID      string
	Expires time.Time
	Showing bool

	Users map[string]*User

	lastResults []CalcResults

	cancels []func()
}

func NewSession(ID string, Expires time.Time, sessionInfo SessionInfo) *Session {
	return &Session{
		SessionInfo: sessionInfo,
		ID:          ID,
		Expires:     Expires,
		Users:       map[string]*User{},
	}
}

func (session *Session) NewUser(name string, userType UserType, isQA bool) *User {
	user := &User{
		UserInfo: UserInfo{
			Name: name,
			Type: userType,
			IsQA: isQA,
		},
		ID:       uuid.NewString(),
		Cards:    map[string]string{},
		UpdateCh: make(chan struct{}),
	}
	session.Users[user.ID] = user
	return user
}

func (session *Session) AllCardsSelected() bool {
	for _, row := range session.Rows {
		for _, user := range session.Users {
			if user.Cards[row] == "" {
				return false
			}
		}
	}
	return true
}

func (session *Session) Calc() []CalcResults {
	if !session.Showing {
		return nil
	}

	var results []CalcResults
	for _, row := range session.Rows {
		result := NewCalcResults(row)

		for _, user := range session.Users {
			if user.Active && user.Type == UserTypeParticipant {
				card := user.Cards[row]
				if card == "" {
					return session.lastResults
				}

				if user.IsQA {
					result.QA.Add(card)
				} else {
					result.Dev.Add(card)
				}
			}
		}

		results = append(results, result)
	}

	if session.MultiRow() {
		summary := NewCalcResults("Summary")
		for _, user := range session.Users {
			if user.Active && user.Type == UserTypeParticipant {
				value := 0.0
				for _, card := range user.Cards {
					amount, _ := strconv.ParseFloat(card, 64)
					value += amount
				}

				cardValue := trimFloat(value)
				if user.IsQA {
					summary.QA.Add(cardValue)
				} else {
					summary.Dev.Add(cardValue)
				}
			}
		}
		results = append([]CalcResults{summary}, results...)
	}

	session.lastResults = results
	return results
}

func (session Session) MultiRow() bool {
	return len(session.Rows) > 1
}

type ReadyUser struct {
	User
	Ready       bool
	Participant bool
}

func (session *Session) ReadyUsers() []ReadyUser {
	var users []ReadyUser
	for _, user := range session.Users {
		users = append(users, ReadyUser{
			User:        *user,
			Ready:       len(user.Cards) == len(session.Rows),
			Participant: user.Type == UserTypeParticipant,
		})
	}

	// Do a bunch of sorts to 1) make it look ordered in the session and 2) keep the same order on updates
	slices.SortFunc(users, func(a, b ReadyUser) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(users, func(a, b ReadyUser) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(users, func(a, b ReadyUser) int {
		if a.Type == b.Type {
			return 0
		}
		if a.Type == UserTypeWatcher {
			return 1
		}
		return -1
	})
	slices.SortFunc(users, func(a, b ReadyUser) int {
		if a.Active == b.Active {
			return 0
		}
		if b.Active {
			return 1
		}
		return -1
	})

	return users
}

func (session *Session) Reset() {
	slog.Info("resetting session", "session", session.ID)
	session.Showing = false
	session.lastResults = nil
	for _, user := range session.Users {
		user.Cards = map[string]string{}
	}
	session.SendUpdates()
}

func (session *Session) DeleteUser(ID string) {
	user := session.Users[ID]
	if user == nil {
		return
	}

	user.Close()
	delete(session.Users, ID)
}

func (session *Session) SendUpdates() {
	slog.Debug("sending session updates", "session", session.ID)
	var wg sync.WaitGroup
	for _, user := range session.Users {
		if user.UpdateCh != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case user.UpdateCh <- struct{}{}:
					slog.Debug("session update sent", "session", session.ID, "user", user.Name)
				case <-time.After(time.Millisecond * 100):
				}
			}()
		}
	}
	wg.Wait()
	slog.Debug("session updates done", "session", session.ID)
}

func (session *Session) WrapContext(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	session.cancels = append(session.cancels, cancel)
	return ctx
}

func (session *Session) Close() {
	for _, cancel := range session.cancels {
		cancel()
	}
	for _, user := range session.Users {
		user.Close()
	}
}

type CalcResults struct {
	Name    string
	Dev, QA Distribution
}

func NewCalcResults(name string) CalcResults {
	results := CalcResults{
		Name: name,
		Dev:  NewDistribution("Dev"),
		QA:   NewDistribution("QA"),
	}
	return results
}

func (r CalcResults) Total() Distribution {
	total := Distribution{counts: map[string]int{}}

	return total
}

type Distribution struct {
	Prefix        string
	count, amount float64
	counts        map[string]int
}

func NewDistribution(prefix string) Distribution {
	return Distribution{Prefix: prefix, counts: map[string]int{}}
}

func (d *Distribution) Add(card string) {
	amount, _ := strconv.ParseFloat(card, 64)

	d.count++
	d.amount += amount
	d.counts[card]++
}

func (d Distribution) Any() bool {
	return d.count > 0
}

func (d Distribution) Avg() float64 {
	if d.count == 0 {
		return 0
	}
	return d.amount / d.count
}

func (d Distribution) Points() string {
	if d.count > 0 {
		return trimFloat(d.Avg())
	}
	return ""
}

func (d Distribution) Distribution() string {
	var counts []string
	for card, count := range d.counts {
		counts = append(counts, fmt.Sprintf("%s(%d)", card, count))
	}
	slices.Sort(counts)
	return strings.Join(counts, " ")
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 2, 64), "0"), ".")
}

type UserType string

var (
	UserTypeParticipant UserType = "Participant"
	UserTypeWatcher     UserType = "Watcher"
)

type UserInfo struct {
	Name string
	Type UserType
	IsQA bool
}

type User struct {
	UserInfo
	Active bool
	ID     string
	Cards  map[string]string

	UpdateCh chan struct{}
}

func (user *User) Close() {
	user.Active = false
	user.UpdateCh = nil
}
