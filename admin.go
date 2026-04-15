package main

import (
	"log"
	"net/http"
	"strconv"
)

func (a *appHandler) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w,"admin_login.html", nil)
}

func (a *appHandler) handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username != a.cfg.AdminUsername || password != a.cfg.AdminPassword {
		renderTemplate(w,"admin_login.html", map[string]string{"Error": "Invalid credentials"})
		return
	}

	token, err := createSession(a.db, "admin")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *appHandler) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	pending, _ := listBookings(a.db, "pending")
	approved, _ := listBookings(a.db, "approved")
	denied, _ := listBookings(a.db, "denied")
	cancelled, _ := listBookings(a.db, "cancelled")

	renderTemplate(w, "admin_dashboard.html", map[string]any{
		"Pending":   pending,
		"Approved":  approved,
		"Denied":    denied,
		"Cancelled": cancelled,
	})
}

func (a *appHandler) handleApprove(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	b, err := getBooking(a.db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := updateBookingStatus(a.db, id, "approved"); err != nil {
		log.Printf("Error approving booking: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	eventID, err := addBookingToCalendar(a.calService, a.cfg.GoogleLifeCalendarID, b)
	if err != nil {
		log.Printf("Error adding to calendar: %v", err)
	} else if eventID != "" {
		setBookingCalendarEvent(a.db, id, eventID)
	}

	go notifyGuestApproved(a.cfg, b)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *appHandler) handleAdminCancel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	b, err := getBooking(a.db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := updateBookingStatus(a.db, id, "cancelled"); err != nil {
		log.Printf("Error cancelling booking: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if err := removeBookingFromCalendar(a.calService, a.cfg.GoogleLifeCalendarID, b); err != nil {
		log.Printf("Error removing calendar event: %v", err)
	}

	go notifyGuestCancelled(a.cfg, b)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *appHandler) handleDeny(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	b, err := getBooking(a.db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := updateBookingStatus(a.db, id, "denied"); err != nil {
		log.Printf("Error denying booking: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	go notifyGuestDenied(a.cfg, b)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
