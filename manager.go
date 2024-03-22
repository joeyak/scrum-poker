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
	return SessionManager{m: map[string]*models.Session{}}
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
