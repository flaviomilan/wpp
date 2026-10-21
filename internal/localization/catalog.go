// Package localization renders application-owned messages without changing stored content.
package localization

import (
	"embed"
	"encoding/json"
	"strings"
)

const English = "en"
const Portuguese = "pt-BR"

// Valid reports whether a language is supported, without consulting the environment.
func Valid(language string) bool { return language == English || language == Portuguese }

// Normalize supplies the deterministic English fallback for missing or legacy values.
func Normalize(language string) string {
	if Valid(language) {
		return language
	}
	return English
}

var catalogs = map[string]map[string]string{
	English:    {"error.generic": "The operation failed. Check the service status.", "language.override_help": "application language for this invocation: en or pt-BR", "language.invalid": "Use en or pt-BR for application language.", "language.saved": "Application language saved.", "count.one": "{count} item", "count.other": "{count} items", "operation.interrupted": "Operation interrupted. Request it again to continue."},
	Portuguese: {"error.generic": "A operação falhou. Confira o estado do serviço.", "language.override_help": "idioma da aplicação nesta execução: en ou pt-BR", "language.invalid": "Use en ou pt-BR para o idioma da aplicação.", "language.saved": "Idioma da aplicação salvo.", "count.one": "{count} item", "count.other": "{count} itens", "operation.interrupted": "Execução interrompida. Solicite novamente para continuar."},
}

// Text renders a catalog message with named, plain-text parameters.
func Text(language, key string, parameters map[string]string) string {
	value := catalogs[Normalize(language)][key]
	if value == "" {
		value = catalogs[English][key]
	}
	if value == "" {
		value = catalogs[English]["error.generic"]
	}
	replacements := make([]string, 0, len(parameters)*2)
	for name, parameter := range parameters {
		replacements = append(replacements, "{"+name+"}", parameter)
	}
	return strings.NewReplacer(replacements...).Replace(value)
}

//go:embed *.json
var embedded embed.FS

func init() {
	files, err := embedded.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		raw, err := embedded.ReadFile(file.Name())
		if err != nil {
			panic(err)
		}
		var messages map[string]map[string]string
		if err = json.Unmarshal(raw, &messages); err != nil {
			panic(err)
		}
		for key, languages := range messages {
			for language, value := range languages {
				catalogs[language][key] = value
			}
		}
	}
}

// Known identifies explicit catalog keys, never matching or translating source prose.
func Known(key string) bool { _, ok := catalogs[English][key]; return ok }
func CodeForKey(key string) string {
	if Known(key) {
		return key
	}
	return ""
}

// Catalogs returns the embedded public application text for browser presentation.
func Catalogs() map[string]map[string]string { return catalogs }
