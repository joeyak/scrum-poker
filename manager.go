package main

import (
	"log/slog"
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

func (manager *SessionManager) New(sessionInfo models.SessionInfo) *models.Session {
	session := models.NewSession(uuid.NewString(), time.Now().Add(time.Hour*24), sessionInfo)
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

func (manager *SessionManager) Cleanup() {
	for ID := range manager.m {
		if manager.m[ID].Expires.Before(time.Now()) {
			slog.Info("closing expired session", "sessionID", ID)
			manager.m[ID].Close()
			delete(manager.m, ID)
		}
	}
}
