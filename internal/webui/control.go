package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"wpp/internal/archive"
	"wpp/internal/service"
)

func decode(w http.ResponseWriter, r *http.Request, value any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return &archive.ValidationError{Message: "Solicitação inválida. Confira os campos."}
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return &archive.ValidationError{Message: "A solicitação deve conter um único objeto."}
	}
	return nil
}
func (s *Server) controls(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/service", func(w http.ResponseWriter, r *http.Request) {
		if s.control == nil {
			respond(w, map[string]any{"owner": false, "linked": false, "connection": "external", "settings": map[string]any{}, "operations": []any{}})
			return
		}
		state, e := s.control.Snapshot()
		if e != nil {
			problem(w, 500, "Não foi possível consultar o serviço.")
			return
		}
		respond(w, state)
	})
	gate := func(w http.ResponseWriter) bool {
		if s.control == nil {
			problem(w, 409, "O painel está em modo de consulta.")
			return false
		}
		return true
	}
	action := func(path string, fn func(*http.Request) (any, error)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !gate(w) {
				return
			}
			result, e := fn(r)
			if e != nil {
				respondError(w, e)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			// The response cannot be recovered after a disconnected client; never log QR/session data.
			_ = json.NewEncoder(w).Encode(result)
		})
	}
	action("POST /api/session/pair", func(r *http.Request) (any, error) { return s.control.Pair() })
	mux.HandleFunc("GET /api/session/qr", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		raw := s.control.QR()
		if len(raw) == 0 {
			problem(w, 404, "QR code indisponível ou expirado.")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		// A disconnected reader needs to request a fresh QR; do not log its bytes.
		_, _ = w.Write(raw)
	})
	action("POST /api/chats/discover", func(r *http.Request) (any, error) { return s.control.Discover() })
	action("POST /api/chats/{chat}/sync", func(r *http.Request) (any, error) { return s.control.Sync(r.PathValue("chat")) })
	action("POST /api/operations/{id}/cancel", func(r *http.Request) (any, error) {
		return map[string]string{"status": "cancelling"}, s.control.Cancel(r.PathValue("id"))
	})
	mux.HandleFunc("PUT /api/chats/{chat}/tracking", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		var body struct {
			Tracked *bool `json:"tracked"`
		}
		if e := decode(w, r, &body); e != nil || body.Tracked == nil {
			problem(w, 400, "Informe tracked como true ou false.")
			return
		}
		if e := s.control.Track(r.PathValue("chat"), *body.Tracked); e != nil {
			respondError(w, e)
			return
		}
		respond(w, map[string]string{"status": "saved"})
	})
	for _, kind := range []string{"capture", "transcription"} {
		mux.HandleFunc("PUT /api/service/"+kind, func(w http.ResponseWriter, r *http.Request) {
			if !gate(w) {
				return
			}
			var body struct {
				Enabled *bool `json:"enabled"`
			}
			if e := decode(w, r, &body); e != nil || body.Enabled == nil {
				problem(w, 400, "Informe enabled como true ou false.")
				return
			}
			var e error
			if kind == "capture" {
				e = s.control.SetCapture(*body.Enabled)
			} else {
				e = s.control.SetTranscription(*body.Enabled)
			}
			if e != nil {
				respondError(w, e)
				return
			}
			respond(w, map[string]string{"status": "saved"})
		})
	}
	mux.HandleFunc("POST /api/exports", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		var body struct {
			Chat string `json:"chat_id"`
		}
		if e := decode(w, r, &body); e != nil {
			problem(w, 400, e.Error())
			return
		}
		op, e := s.control.Export(body.Chat)
		if e != nil {
			respondError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		// A failed response write cannot change the accepted background operation.
		_ = json.NewEncoder(w).Encode(op)
	})
	mux.HandleFunc("GET /api/service/settings", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		state, e := s.control.Snapshot()
		if e != nil {
			problem(w, 500, "Falha ao consultar ajustes.")
			return
		}
		respond(w, state.Settings)
	})
	mux.HandleFunc("PUT /api/service/settings", func(w http.ResponseWriter, r *http.Request) {
		if !gate(w) {
			return
		}
		var config service.Settings
		if e := decode(w, r, &config); e != nil {
			problem(w, 400, e.Error())
			return
		}
		op, e := s.control.Configure(config)
		if e != nil {
			respondError(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		// A failed response write cannot change the accepted background operation.
		_ = json.NewEncoder(w).Encode(op)
	})
}
