package main

import (
	"log"
	"net/http"
	"strconv"
)

func (a *appHandler) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "admin_login.html", nil)
}

func (a *appHandler) handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username != a.cfg.AdminUsername || password != a.cfg.AdminPassword {
		renderTemplate(w, "admin_login.html", map[string]string{"Error": "Invalid credentials"})
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

	// Claim the booking before doing any calendar work. Only a pending booking can be
	// approved, and only once: an already-approved booking has its own event on the very
	// calendar the type is re-derived from, and a cancelled one may still if removal
	// failed — either way the re-read returns zero cat-sitting days and would persist a
	// cat_sitting -> regular downgrade, alongside a duplicate event and an orphaned id.
	claimed, err := claimBookingForApproval(a.db, id)
	if err != nil {
		log.Printf("Error approving booking %d: %v", id, err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if !claimed {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	b.Status = "approved"

	// Determined before the booking lands on the calendar, so its own event cannot
	// influence the host-availability read. Host travel may have been added or dropped
	// since the request was submitted, so re-derive the type and persist it — otherwise
	// the stored type, the calendar event title, the badge, and the email disagree.
	//
	// Only on a successful read: an error here means "could not determine", and treating
	// that as "no cat-sitting days" would persist a downgrade of a real cat-sitting
	// booking on nothing more than a transient calendar failure.
	catDates, err := a.catSittingDatesForBooking(b)
	if err != nil {
		log.Printf("Error re-deriving cat-sitting dates for booking %d, keeping stored type %q: %v", id, b.BookingType, err)
		catDates = nil
	} else {
		bookingType := bookingTypeRegular
		if len(catDates) > 0 {
			bookingType = bookingTypeCatSitting
		}
		if b.BookingType != bookingType {
			if err := updateBookingType(a.db, id, bookingType); err != nil {
				// Leave b untouched, so the event title and email match what is stored.
				log.Printf("Error updating booking type for %d: %v", id, err)
			} else {
				b.BookingType = bookingType
			}
		}
	}

	eventID, err := addBookingToCalendar(a.calService, a.cfg.GoogleLifeCalendarID, b)
	if err != nil {
		log.Printf("Error adding to calendar: %v", err)
	} else if eventID != "" {
		setBookingCalendarEvent(a.db, id, eventID)
	}

	go notifyGuestApproved(a.cfg, b, catDates)

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
		// The event is still out there. Keep the id so it can be retried or removed by
		// hand, rather than clearing it and losing the only handle on it.
		log.Printf("Error removing calendar event: %v", err)
	} else if b.CalendarEventID != "" {
		if err := clearBookingCalendarEvent(a.db, id); err != nil {
			log.Printf("Error clearing calendar event id for %d: %v", id, err)
		}
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
