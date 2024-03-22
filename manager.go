package main

import (
	"time"

	"github.com/google/uuid"
	"github.com/joeyak/scrum-poker/models"
)

type SessionManager struct {
	m map[string]*models.Session
}

func NewSessionManager() SessionManager {
	return SessionManager{m: map[string]*models.Session{
		"96fafc6f-9919-4df5-b5c1-325b2caba01f": {
			ID:      "96fafc6f-9919-4df5-b5c1-325b2caba01f",
			Expires: time.Now().Add(time.Hour * 1000000),
			Cards:   []string{"0.5", "1", "2", "3", "4", "5", "6", "7", "8"},
			Users: map[string]*models.User{
				"7ab54fb9-ebdb-4580-9557-16e2995837cc": {
					ID:       "7ab54fb9-ebdb-4580-9557-16e2995837cc",
					Name:     "Joey",
					Type:     models.UserTypeParticipant,
					UpdateCh: make(chan struct{}),
					Active:   true,
				},
			},
		},
	}}
}

func (manager *SessionManager) New(cards []string) *models.Session {
	session := models.NewSession(uuid.NewString(), time.Now().Add(time.Minute*10), cards)
	manager.m[session.ID] = session
	return session
}

func (manager *SessionManager) Get(ID string) *models.Session {
	session := manager.m[ID]
	if session == nil || time.Now().After(session.Expires) {
		delete(manager.m, ID)
		return nil
	}
	return session
}
