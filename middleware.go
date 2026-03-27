package main

import (
	"database/sql"
	"net/http"

	"google.golang.org/api/calendar/v3"
)

type appHandler struct {
	db         *sql.DB
	cfg        *Config
	calService *calendar.Service
}

func (a *appHandler) requireGuest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		role, ok := getSession(a.db, token)
		if !ok {
			clearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if role != "guest" && role != "admin" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *appHandler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token == "" {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		role, ok := getSession(a.db, token)
		if !ok {
			clearSessionCookie(w)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if role != "admin" {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
