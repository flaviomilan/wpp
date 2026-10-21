package webui

import (
	"net/http"
	"wpp/internal/localization"
)

func (s *Server) preferences(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		language, err := s.Store.ApplicationLanguage()
		if err != nil {
			respondError(w, err)
			return
		}
		s.preferencesMu.RLock()
		effective := language
		if s.languageOverride != "" {
			effective = s.languageOverride
		}
		s.preferencesMu.RUnlock()
		respond(w, map[string]string{"application_language": language, "initial_language": effective})
	})
	mux.HandleFunc("PUT /api/preferences", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Language string `json:"application_language"`
		}
		if err := decode(w, r, &input); err != nil {
			respondError(w, err)
			return
		}
		if !localization.Valid(input.Language) {
			problemMessage(w, 400, "language.invalid")
			return
		}
		if err := s.Store.SaveApplicationLanguage(input.Language); err != nil {
			respondError(w, err)
			return
		}
		s.preferencesMu.Lock()
		s.languageOverride = ""
		s.preferencesMu.Unlock()
		respond(w, map[string]string{"application_language": input.Language})
	})
	mux.HandleFunc("GET /api/translations", func(w http.ResponseWriter, r *http.Request) { respond(w, localization.Catalogs()) })
}
