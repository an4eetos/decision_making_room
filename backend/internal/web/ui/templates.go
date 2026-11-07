package ui

import (
	"html/template"
	"strings"
)

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"truncate": func(s string, n int) string {
			s = strings.TrimSpace(s)
			runes := []rune(s)
			if len(runes) <= n {
				return s
			}
			return string(runes[:n]) + "..."
		},
		"join": strings.Join,
	}
}
