package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	defaultCards = []string{"1", "2", "3", "5", "8", "13"}

	//go:embed static/*
	staticFS embed.FS

	sessionManager = NewSessionManager()
)

func main() {
	var addr string
	var debugLog, logEndpoints, noColor bool
	flag.StringVar(&addr, "addr", "0.0.0.0:8080", "Server Address")
	flag.BoolVar(&debugLog, "debug", false, "Enable Debug Logging")
	flag.BoolVar(&noColor, "no-color", false, "No Color Output")
	flag.BoolVar(&logEndpoints, "log-endpoints", false, "Log Endpoints")
	flag.Parse()

	level := slog.LevelInfo
	if debugLog {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:     level,
			AddSource: true,
			NoColor:   noColor,
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

	mux := Handler{mux: http.NewServeMux(), logEndpoints: logEndpoints}
	mux.HandleFunc("/", htmxMiddleware(handleRoot))
	mux.HandleFunc("GET /static/", handleStatic)
	mux.HandleFunc("POST /new", handleNewSession)
	mux.HandleFunc("GET /session/{sessionID}", htmxMiddleware(handleSession))
	mux.HandleFunc("POST /session/{sessionID}", htmxMiddleware(handleSession))
	mux.HandleFunc("POST /session/{sessionID}/join", handleSessionJoin)
	mux.HandleFunc("/session/{sessionID}/user/{userID}/exit", handleSessionExit)
	mux.HandleFunc("/session/{sessionID}/user/{userID}/ws", handleUserWs)

	slog.Info("Starting server", "addr", addr, "debug", debugLog, "noColor", noColor, "logEndpoints", logEndpoints)
	http.ListenAndServe(addr, mux)
}

type Handler struct {
	mux          *http.ServeMux
	logEndpoints bool
}

func (h Handler) HandleFunc(pattern string, handler http.HandlerFunc, middlewares ...func(http.HandlerFunc) http.HandlerFunc) {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}
	h.mux.HandleFunc(pattern, handler)
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.logEndpoints {
		ips := r.Header.Get("X-Forwarded-For")
		if ips == "" {
			ips = r.RemoteAddr
		}
		slog.Info("endpoint hit", "ip", strings.Split(ips, ","), "path", r.RequestURI)
	}
	h.mux.ServeHTTP(w, r)
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

	info := getInfoCookie(r)
	if r.URL.Query().Has("cards") {
		info.Session.Cards = strings.Split(r.URL.Query().Get("cards"), ",")
	}
	if r.URL.Query().Has("rows") {
		info.Session.Rows = strings.Split(r.URL.Query().Get("rows"), ",")
	}

	err := components.RootPage(info, "").Render(r.Context(), w)
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
	cards := strings.Split(r.FormValue("cards"), ",")
	rows := strings.Split(r.FormValue("rows"), ",")

	info := getInfoCookie(r)
	info.Session.Cards = cards
	info.Session.Rows = rows

	errorResponse := func(message string, err error) {
		slog.Error(message, err)
		err = components.RootPage(info, strings.ToUpper(message[0:1])+message[1:]).Render(r.Context(), w)
		if err != nil {
			slog.Error("could not render root page", err)
		}
	}

	for _, s := range cards {
		_, err := strconv.ParseFloat(s, 64)
		if err != nil {
			errorResponse("invalid cards value", err)
			return
		}
	}

	setInfoCookie(w, info)
	session := sessionManager.New(cards, rows)

	err := components.SessionCreated(*session, r.Header.Get("Origin")).Render(r.Context(), w)
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
		info := getInfoCookie(r)

		err := components.SessionJoin(*session, info).Render(r.Context(), w)
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
		http.SetCookie(w, &http.Cookie{Name: session.ID, Path: "/", MaxAge: -1})
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

		setInfoCookie(w, models.CookieData{
			User:    user.UserInfo,
			Session: session.SessionInfo,
		})
	}

	http.Redirect(w, r, fmt.Sprintf("/session/%s", session.ID), http.StatusFound)
}

func handleSessionExit(w http.ResponseWriter, r *http.Request) {
	session := sessionManager.Get(r.PathValue("sessionID"))
	if session == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	user := session.Users[r.PathValue("userID")]
	if user != nil {
		slog.Info("removing user from session", "session", session.ID, "user", user.Name)
		user.Active = false
		close(user.UpdateCh)
		delete(session.Users, user.ID)
		session.SendUpdates()
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
	defer func() {
		slog.Debug("ws connection closing", logAttrs)
		user.Active = false
		session.SendUpdates()
	}()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Error("could not accept websocket connection", logAttrs, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer conn.CloseNow()

	renderError := func(message string, redirect bool) {
		redirectLink := ""
		if redirect {
			redirectLink = fmt.Sprintf("/session/%s", session.ID)
		}

		var buff bytes.Buffer
		err := components.PokerError(message, redirectLink).Render(r.Context(), &buff)
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
				if !errors.As(err, &websocket.CloseError{}) && !errors.Is(err, io.EOF) {
					slog.Debug("could not read connection", logAttrs, err)
				}
				cancel()
				return
			}

			value := struct {
				Card, Row     string
				UndoSelection bool
				FlipType      bool
				FlipQA        bool
				ShowResults   bool
				ResetResults  bool
			}{}
			err = json.Unmarshal(message, &value)
			if err != nil {
				slog.Error("could not unmarshal value", logAttrs, err)
				renderError("An error occured while retrieving data", false)
				continue
			}

			if value.ResetResults {
				session.Reset()
				continue
			}

			if value.Card != "" {
				user.Cards[value.Row] = value.Card
				if value.UndoSelection {
					delete(user.Cards, value.Row)
				}
			}

			if value.FlipQA {
				user.IsQA = !user.IsQA
			}

			if value.FlipType {
				if user.Type == models.UserTypeParticipant {
					user.Type = models.UserTypeWatcher
					clear(user.Cards)
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
		case _, ok := <-user.UpdateCh:
			if !ok {
				renderError("Your connection has been forcibly closed. Redirecting...", true)
				return
			}
		case <-ctx.Done():
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

func getInfoCookie(r *http.Request) models.CookieData {
	info := models.CookieData{Session: models.NewSessionInfo(defaultCards, nil)}
	if cookie, _ := r.Cookie("info"); cookie != nil {
		data, err := base64.StdEncoding.DecodeString(cookie.Value)
		if err == nil {
			json.Unmarshal(data, &info)
		}
	}
	return info
}

func setInfoCookie(w http.ResponseWriter, info models.CookieData) {
	data, err := json.Marshal(info)
	if err != nil {
		slog.Error("could not marshal info", err)
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:  "info",
			Value: base64.StdEncoding.EncodeToString(data),
			Path:  "/",
		})
	}
}
