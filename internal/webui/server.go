package webui

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"wpp/internal/localization"

	"wpp/internal/archive"
	"wpp/internal/service"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	Store            *archive.Store
	token            string
	port             string
	zone             string
	control          *service.Controller
	languageOverride string
	preferencesMu    sync.RWMutex
}

func New(store *archive.Store, port string, controls ...*service.Controller) *Server {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	s := &Server{Store: store, token: hex.EncodeToString(raw[:]), port: port, zone: "America/Sao_Paulo"}
	if len(controls) > 0 {
		s.control = controls[0]
	}
	return s
}

func ServeControlled(ctx context.Context, store *archive.Store, address string, ready func(string), zone *time.Location, control *service.Controller, languages ...string) error {
	return serveControlled(ctx, store, address, ready, zone, control, net.Listen, languages...)
}

func serveControlled(ctx context.Context, store *archive.Store, address string, ready func(string), zone *time.Location, control *service.Controller, listen func(string, string) (net.Listener, error), languages ...string) (resultErr error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return localization.New("backend.invalid_address_use_127_0_0_1_8080")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return localization.New("backend.the_interface_accepts_only_a_loopback_ip_address_such_as")
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return err
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	ui := New(store, port, control)
	ui.zone = zone.String()
	if len(languages) > 0 && localization.Valid(languages[0]) {
		ui.languageOverride = languages[0]
	}
	server := &http.Server{Handler: ui.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	ready("http://" + listener.Addr().String())
	return serveHTTP(ctx, server, listener, 5*time.Second)

}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.controls(mux)
	s.preferences(mux)
	mux.HandleFunc("GET /api/audio", func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := pagination(r)
		if err != nil {
			problemError(w, 400, err)
			return
		}
		jobs, summary, err := s.Store.BrowseAudioPage(offset, limit+1, r.URL.Query().Get("chat"), r.URL.Query().Get("state"))
		if err != nil {
			respondError(w, err)
			return
		}

		more := len(jobs) > limit
		if more {
			jobs = jobs[:limit]
		}
		respond(w, map[string]any{"items": jobs, "summary": summary, "offset": offset, "more": more})
	})
	mux.HandleFunc("POST /api/audio/{source}/retry", func(w http.ResponseWriter, r *http.Request) {

		if s.control != nil {
			state, err := s.control.Snapshot()
			if err != nil {
				respondError(w, err)
				return
			}
			if state.Owner {
				op, err := s.control.RetryAudio(r.PathValue("source"))
				if err != nil {
					respondError(w, err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				// Disconnected clients cannot receive another response; the operation remains persisted.
				_ = json.NewEncoder(w).Encode(op)
				return
			}
		}
		if err := s.Store.RetryAudio(r.PathValue("source")); err != nil {
			respondError(w, err)
			return
		}
		respond(w, map[string]string{"status": "pending", "detail": localization.Text(localization.English, "backend.audio_requeued_the_external_controller_will_process_it_when_enabled", nil), "detail_code": "backend.audio_requeued_the_external_controller_will_process_it_when_enabled"})
	})
	mux.HandleFunc("GET /api/audio/{source}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Store.BrowserAudioSource(r.PathValue("source"))
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, item)
	})
	mux.HandleFunc("GET /api/messages/{source}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Store.BrowserSource(r.PathValue("source"))
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, item)
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { respond(w, map[string]string{"timezone": s.zone}) })
	mux.HandleFunc("GET /api/chats", func(w http.ResponseWriter, r *http.Request) {
		chats, err := s.Store.Chats()
		if err != nil {
			respondError(w, err)
			return
		}
		for i := range chats {
			chats[i].Seed = nil
		}
		respond(w, chats)
	})
	mux.HandleFunc("GET /api/chats/{chat}/messages", func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := pagination(r)
		if err != nil {
			problemError(w, 400, err)
			return
		}
		page, err := s.Store.BrowseMessages(r.PathValue("chat"), r.URL.Query().Get("q"), r.URL.Query().Get("person"), offset, limit)
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, page)
	})
	mux.HandleFunc("GET /api/groups/{group}/people", func(w http.ResponseWriter, r *http.Request) {
		people, err := s.Store.People(r.PathValue("group"))
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, people)
	})
	mux.HandleFunc("POST /api/groups/{group}/people", func(w http.ResponseWriter, r *http.Request) {
		var person struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&person); err != nil {
			problemMessage(w, 400, "backend.invalid_person_registration_json")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			problemMessage(w, 400, "backend.registration_must_contain_a_single_object")
			return
		}
		if err := s.Store.SetPersonName(r.PathValue("group"), person.ID, person.Name); err != nil {
			respondError(w, err)
			return
		}
		respond(w, map[string]string{"status": "saved"})
	})
	mux.HandleFunc("GET /api/tables", func(w http.ResponseWriter, r *http.Request) {
		tables, err := s.Store.Tables()
		if err != nil {
			respondError(w, err)
			return
		}
		sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
		respond(w, tables)
	})
	mux.HandleFunc("GET /api/tables/{table}", func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := pagination(r)
		if err != nil {
			problemError(w, 400, err)
			return
		}
		page, err := s.Store.InspectTable(r.PathValue("table"), offset, limit)
		if err != nil {
			respondError(w, err)
			return
		}
		respond(w, page)
	})
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if !publicAsset(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "wpp_ui_" + s.port, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil || port != s.port || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			problemMessage(w, 403, "backend.invalid_host")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			cookie, err := r.Cookie("wpp_ui_" + s.port)
			if err != nil || cookie.Value != s.token {
				problem(w, 401, "abra a interface no navegador antes de consultar")
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != "http://"+r.Host {
				problemMessage(w, 403, "backend.invalid_mutation_origin")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func pagination(r *http.Request) (int, int, error) {
	offset := 0
	limit := 50
	var err error
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, localization.New("backend.invalid_offset")
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, localization.New("backend.invalid_limit")
		}
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return 0, 0, localization.New("backend.invalid_pagination")
	}
	return offset, limit, nil
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// HTTP write failures have no recoverable response and must not expose message content in logs.
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// A failed error response cannot be followed by another response.
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func publicAsset(path string) bool {
	switch path {
	case "/messages.mjs", "/preferences.mjs", "/i18n.mjs", "/", "/app.js", "/style.css", "/api.mjs", "/dom.mjs", "/requests.mjs", "/chats.mjs", "/people.mjs", "/audio.mjs", "/settings.mjs", "/activity.mjs", "/inspector.mjs", "/navigation.mjs":
		return true
	default:
		return false
	}
}
