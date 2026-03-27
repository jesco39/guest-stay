package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

const sessionCookieName = "session"
const sessionDuration = 24 * time.Hour

func createSession(db *sql.DB, role string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	expiresAt := time.Now().Add(sessionDuration).Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO sessions (token, role, expires_at) VALUES (?, ?, ?)`,
		token, role, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func getSession(db *sql.DB, token string) (string, bool) {
	var role, expiresAt string
	err := db.QueryRow(
		`SELECT role, expires_at FROM sessions WHERE token = ?`, token,
	).Scan(&role, &expiresAt)
	if err != nil {
		return "", false
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
		return "", false
	}
	return role, true
}

func deleteSession(db *sql.DB, token string) {
	db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
}

func cleanExpiredSessions(db *sql.DB) {
	db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Format(time.RFC3339))
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func getSessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
