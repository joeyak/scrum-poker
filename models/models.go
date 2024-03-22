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

type Session struct {
	ID      string
	Expires time.Time
	Cards   []string
	Showing bool

	Users map[string]*User

	cancels []func()
}

func NewSession(ID string, Expires time.Time, cards []string) *Session {
	return &Session{
		ID:      ID,
		Expires: Expires,
		Cards:   cards,
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
		UpdateCh: make(chan struct{}),
	}
	session.Users[user.ID] = user
	return user
}

func (session *Session) Calc() (CalcResults, bool) {
	result := NewCalcResults()

	for _, user := range session.Users {
		if user.Active && user.Type == UserTypeParticipant {
			if user.Card == "" {
				return CalcResults{}, false
			}

			result.Add(user.Card, user.IsQA)
		}
	}

	return result, true
}

func (session *Session) SortedUsers() []*User {
	var users []*User
	for _, user := range session.Users {
		users = append(users, user)
	}

	// Do a bunch of sorts to 1) make it look ordered in the session and 2) keep the same order on updates
	slices.SortFunc(users, func(a, b *User) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(users, func(a, b *User) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(users, func(a, b *User) int {
		if a.Type == b.Type {
			return 0
		}
		if a.Type == UserTypeWatcher {
			return 1
		}
		return -1
	})
	slices.SortFunc(users, func(a, b *User) int {
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
		user.Card = ""
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
	count, total, qaCount, qaTotal float64
	distribution, qaDistribution   map[string]int
}

func NewCalcResults() CalcResults {
	return CalcResults{
		distribution:   map[string]int{},
		qaDistribution: map[string]int{},
	}
}

func (results *CalcResults) Add(card string, qa bool) {
	amount, _ := strconv.ParseFloat(card, 64)

	results.count++
	results.total += amount
	results.distribution[card]++

	if qa {
		results.qaCount++
		results.qaTotal += amount
		results.qaDistribution[card]++
	}
}

func (results CalcResults) Points() string {
	if results.count > 0 {
		return strconv.FormatFloat(results.total/results.count, 'f', 1, 64)
	}
	return ""
}

func (results CalcResults) QAPoints() string {
	if results.qaCount > 0 {
		return strconv.FormatFloat(results.qaTotal/results.qaCount, 'f', 1, 64)
	}
	return ""
}

func (results CalcResults) Distribution() string {
	return results.getDistribution(results.distribution)
}

func (results CalcResults) QADistribution() string {
	return results.getDistribution(results.qaDistribution)
}

func (results CalcResults) getDistribution(m map[string]int) string {
	var counts []string
	for card, count := range m {
		counts = append(counts, fmt.Sprintf("%s(%d)", card, count))
	}
	return strings.Join(counts, " ")
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
	Card   string

	UpdateCh chan struct{}
}
