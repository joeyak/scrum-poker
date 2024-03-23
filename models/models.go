package models

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CookieData struct {
	User    UserInfo
	Session SessionInfo
}

type SessionInfo struct {
	Cards []string
	Rows  []string
}

type Session struct {
	SessionInfo
	ID      string
	Expires time.Time
	Showing bool

	Users map[string]*User

	cancels []func()
}

func NewSession(ID string, Expires time.Time, cards, rows []string) *Session {
	if len(rows) == 0 {
		rows = []string{""}
	}
	return &Session{
		SessionInfo: SessionInfo{
			Cards: cards,
			Rows:  rows,
		},
		ID:      ID,
		Expires: Expires,
		Users:   map[string]*User{},
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

func (session *Session) Calc() ([]CalcResults, bool) {
	var results []CalcResults
	for _, row := range session.Rows {
		result := NewCalcResults(row)

		for _, user := range session.Users {
			if user.Active && user.Type == UserTypeParticipant {
				card := user.Cards[row]
				if card == "" {
					return nil, false
				}

				result.Add(card, user.IsQA)
			}
		}

		results = append(results, result)
	}

	if len(session.Rows) > 1 {
		total := NewCalcResults("Total")
		for _, user := range session.Users {
			if user.Active && user.Type == UserTypeParticipant {
				value := 0.0
				for _, card := range user.Cards {
					amount, _ := strconv.ParseFloat(card, 64)
					value += amount
				}
				total.Add(trimFloat(value), user.IsQA)
			}
		}
		results = append([]CalcResults{total}, results...)
	}

	return results, true
}

type ReadyUser struct {
	User
	Ready bool
}

func (session *Session) ReadyUsers() []ReadyUser {
	var users []ReadyUser
	for _, user := range session.Users {
		users = append(users, ReadyUser{
			User:  *user,
			Ready: len(user.Cards) == len(session.Rows),
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
	for _, user := range session.Users {
		user.Cards = map[string]string{}
	}
	session.SendUpdates()
}

func (session *Session) SendUpdates() {
	slog.Debug("sending session updates", "session", session.ID)
	for _, user := range session.Users {
		select {
		case user.UpdateCh <- struct{}{}:
			slog.Debug("session update sent", "session", session.ID, "user", user.Name)
		case <-time.After(time.Millisecond * 100):
		}
	}
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
}

type CalcResults struct {
	Name                               string
	count, total, qaCount, qaTotal     float64
	normalDistribution, qaDistribution map[string]int
}

func NewCalcResults(name string) CalcResults {
	return CalcResults{
		Name:               name,
		normalDistribution: map[string]int{},
		qaDistribution:     map[string]int{},
	}
}

func (results *CalcResults) Add(card string, qa bool) {
	amount, _ := strconv.ParseFloat(card, 64)

	results.count++
	results.total += amount
	results.normalDistribution[card]++

	if qa {
		results.qaCount++
		results.qaTotal += amount
		results.qaDistribution[card]++
	}
}

func (results CalcResults) Points() string {
	if results.count > 0 {
		return trimFloat(results.total / results.count)
	}
	return ""
}

func (results CalcResults) QAPoints() string {
	if results.qaCount > 0 {
		return trimFloat(results.qaTotal / results.qaCount)
	}
	return ""
}

func (results CalcResults) Distribution() string {
	return results.distribution(results.normalDistribution)
}

func (results CalcResults) QADistribution() string {
	return results.distribution(results.qaDistribution)
}

func (results CalcResults) distribution(m map[string]int) string {
	var counts []string
	for card, count := range m {
		counts = append(counts, fmt.Sprintf("%s(%d)", card, count))
	}
	return strings.Join(counts, " ")
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 1, 64), "0"), ".")
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
