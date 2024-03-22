package models

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID      string
	Expires time.Time
	Cards   []string

	Users map[string]*User
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
		ID:   uuid.NewString(),
		Name: name,
		Type: userType,
		IsQA: isQA,
	}
	session.Users[user.ID] = user
	return user
}

func (session *Session) SortedUsers() []*User {
	var users []*User
	for _, user := range session.Users {
		users = append(users, user)
	}
	slices.SortFunc(users, func(a, b *User) int { return strings.Compare(a.Name, b.Name) })
	return users
}

type UserType string

var (
	UserTypeParticipant UserType = "Participant"
	UserTypeWatcher     UserType = "Watcher"
)

type User struct {
	ID   string
	Name string
	Type UserType
	IsQA bool
	Card float64
}
