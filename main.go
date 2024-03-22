package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/angelofallars/htmx-go"
	"github.com/joeyak/scrum-poker/components"
	"github.com/joeyak/scrum-poker/models"
	"github.com/lmittmann/tint"
	"nhooyr.io/websocket"
)

var (
	// Default Room Settings
	defaultCards = "1,2,3,5,8,13,21"

	//go:embed static/*
	staticFS embed.FS

	sessionManager = NewSessionManager()
)

func main() {
	var addr string
	var debugLog bool
	flag.StringVar(&addr, "addr", "0.0.0.0:8080", "Server Address")
	flag.StringVar(&defaultCards, "cards", defaultCards, "Default Cards")
	flag.BoolVar(&debugLog, "debug", false, "Enable Debug Logging")
	flag.Parse()

	level := slog.LevelInfo
	if debugLog {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:     level,
			AddSource: true,
			ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
				if attr.Key == "!BADKEY" && attr.Value.Kind() == slog.KindAny {
					if err, ok := attr.Value.Any().(error); ok {
						attr = tint.Err(err)
					}
				}
				return attr
			},
		}),
	))

	mux := http.NewServeMux()
	mux.HandleFunc("/", htmxMiddleware(handleRoot))
	mux.HandleFunc("GET /static/", handleStatic)
	mux.HandleFunc("POST /new", handleNewSession)
	mux.HandleFunc("GET /session/{sessionID}", htmxMiddleware(handleSession))
	mux.HandleFunc("POST /session/{sessionID}", htmxMiddleware(handleSession))
	mux.HandleFunc("POST /session/{sessionID}/join", handleSessionJoin)
	mux.HandleFunc("/session/{sessionID}/user/{userID}/ws", handleUserWs)

	http.ListenAndServe(addr, mux)
}

func htmxMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !htmx.IsHTMX(r) {
			err := components.BaseHTML(r.RequestURI).Render(r.Context(), w)
			if err != nil {
				slog.Error("could not render root page", err)
			}
			return
		}

		handler(w, r)
	})
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		err := components.StatusPage(http.StatusNotFound).Render(r.Context(), w)
		if err != nil {
			slog.Error("could not render 404 page", err)
		}
		return
	}

	cards := r.URL.Query().Get("cards")
	if cards == "" {
		cards = defaultCards
	}

	err := components.RootPage(cards, "").Render(r.Context(), w)
	if err != nil {
		slog.Error("could not render root page", err)
	}
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/")
	body, err := staticFS.ReadFile(file)

	if err != nil {
		slog.Error("could not handle static file", err, "file", file)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	mediaType, _, _ := mime.ParseMediaType(mime.TypeByExtension(filepath.Ext(file)))
	w.Header().Set("Content-Type", mediaType)
	w.Write(body)
}

func handleNewSession(w http.ResponseWriter, r *http.Request) {
	cardsText := r.FormValue("cards")

	errorResponse := func(message string, err error) {
		slog.Error(message, err)
		err = components.RootPage(cardsText, strings.ToUpper(message[0:1])+message[1:]).Render(r.Context(), w)
		if err != nil {
			slog.Error("could not render root page", err)
		}
	}

	cards := strings.Split(cardsText, ",")
	for _, s := range cards {
		_, err := strconv.ParseFloat(s, 64)
		if err != nil {
			errorResponse("invalid cards value", err)
			return
		}
	}

	session := sessionManager.New(cards)
	err := components.SessionCreated(*session, r.Host).Render(r.Context(), w)
	if err != nil {
		slog.Error("could not render root page", err)
	}
}

func handleSession(w http.ResponseWriter, r *http.Request) {
	session := sessionManager.Get(r.PathValue("sessionID"))
	if session == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	sessionAttr := slog.String("session", session.ID)

	renderSessionJoin := func() {
		err := components.SessionJoin(*session).Render(r.Context(), w)
		if err != nil {
			slog.Error("could not render session join page", sessionAttr, err)
		}
	}

	userCookie, err := r.Cookie(session.ID)
	if err != nil {
		renderSessionJoin()
		return
	}

	user := session.Users[userCookie.Value]
	if user == nil {
		http.SetCookie(w, &http.Cookie{
			Name:   session.ID,
			Path:   "/",
			MaxAge: -1,
		})
		renderSessionJoin()
		return
	}

	user.Active = true
	err = components.SessionRoom(*session, *user).Render(r.Context(), w)
	if err != nil {
		slog.Error("could not render root page", sessionAttr, "user", user.Name, err)
	}

	go session.SendUpdates()
}

func handleSessionJoin(w http.ResponseWriter, r *http.Request) {
	session := sessionManager.Get(r.PathValue("sessionID"))
	if session == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	_, err := r.Cookie(session.ID)
	if err != nil {
		user := session.NewUser(r.FormValue("name"), models.UserType(r.FormValue("type")), r.Form.Has("isQA"))
		slog.Info("user joined", "session", session.ID, "name", user.Name, "type", user.Type, "qa", user.IsQA)

		http.SetCookie(w, &http.Cookie{
			Name:    session.ID,
			Expires: session.Expires,
			Value:   user.ID,
			Path:    "/",
		})
	}

	http.Redirect(w, r, fmt.Sprintf("/session/%s", session.ID), http.StatusFound)
}

func handleUserWs(w http.ResponseWriter, r *http.Request) {
	session := sessionManager.Get(r.PathValue("sessionID"))
	if session == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	user := session.Users[r.PathValue("userID")]
	if user == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	logAttrs := slog.Group("", slog.String("session", session.ID), slog.String("user", user.Name))
	slog.Debug("ws connection opening", logAttrs)

	defer func() {
		slog.Debug("ws connection closing", logAttrs)
		user.Active = false
		session.SendUpdates()
	}()

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Error("could not accept websocket connection", logAttrs, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conn.CloseNow()

	renderError := func() {
		var buff bytes.Buffer
		err := components.PokerError("An error occured").Render(r.Context(), &buff)
		if err != nil {
			slog.Error("could not render poker error", logAttrs, err)
		}

		err = conn.Write(r.Context(), websocket.MessageText, buff.Bytes())
		if err != nil {
			slog.Error("could not write to websocket for poker error", logAttrs, err)
		}
	}

	user.Active = true
	session.SendUpdates()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		for {
			_, message, err := conn.Read(r.Context())
			if err != nil {
				if !errors.As(err, &websocket.CloseError{}) {
					slog.Error("could not read connection", logAttrs, err)
				}
				cancel()
				return
			}

			slog.Debug("Websock data recieved", logAttrs, "message", string(message))

			value := struct {
				Card          string
				UndoSelection bool
				FlipType      bool
				FlipQA        bool
				ShowResults   bool
				ResetResults  bool
			}{}
			err = json.Unmarshal(message, &value)
			if err != nil {
				slog.Error("could not unmarshal value", logAttrs, err)
				renderError()
				continue
			}

			if value.ResetResults {
				session.Reset()
				continue
			}

			if value.Card != "" {
				user.Card = value.Card
				if value.UndoSelection {
					user.Card = ""
				}
			}

			if value.FlipQA {
				user.IsQA = !user.IsQA
			}

			if value.FlipType {
				if user.Type == models.UserTypeParticipant {
					user.Type = models.UserTypeWatcher
				} else {
					user.Type = models.UserTypeParticipant
				}
			}

			session.Showing = false
			if value.ShowResults {
				session.Showing = true
			}

			session.SendUpdates()
		}
	}()

	for {
		select {
		case <-user.UpdateCh:
		case <-ctx.Done():
			slog.Debug("connection closed", logAttrs)
			return
		}

		var buff bytes.Buffer
		err = components.PokerContent(*session, *user).Render(r.Context(), &buff)
		if err != nil {
			slog.Error("could not render poker content", logAttrs, err)
		}

		err = conn.Write(r.Context(), websocket.MessageText, buff.Bytes())
		if err != nil {
			slog.Error("could not write to websocket connection for poker content", logAttrs, err)
		}
	}
}
