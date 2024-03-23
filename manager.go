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
		"cf7e5d11-45b9-401a-8c13-4ddddab765d8": {
			SessionInfo: models.SessionInfo{
				Cards: []string{"1", "2", "3"},
				Rows:  []string{"Familiarity", "Complexity", "Size"},
			},
			ID:      "cf7e5d11-45b9-401a-8c13-4ddddab765d8",
			Expires: time.Now().Add(time.Hour * 200000),
			Users: map[string]*models.User{
				"72777d67-907a-40b6-8735-73f0c290f0f8": {
					ID: "72777d67-907a-40b6-8735-73f0c290f0f8",
					UserInfo: models.UserInfo{
						Name: "Joey",
						Type: models.UserTypeParticipant,
					},
					Cards:    map[string]string{},
					UpdateCh: make(chan struct{}),
				},
			},
		},
	}}
}

func (manager *SessionManager) New(cards, rows []string) *models.Session {
	session := models.NewSession(uuid.NewString(), time.Now().Add(time.Hour*24), cards, rows)
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
