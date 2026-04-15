package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

var pageTemplates map[string]*template.Template

var funcMap = template.FuncMap{
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
	"add": func(a, b int) int { return a + b },
	"weekday": func(d time.Weekday) int { return int(d) },
	"monthName": func(m time.Month) string { return m.String() },
}

func initTemplates() {
	layout := template.Must(template.New("layout").Funcs(funcMap).ParseFiles("templates/layout.html"))

	pages := []string{
		"templates/guest_login.html",
		"templates/calendar.html",
		"templates/booking_form.html",
		"templates/booking_confirm.html",
		"templates/booking_status.html",
		"templates/admin_login.html",
		"templates/admin_dashboard.html",
	}

	pageTemplates = make(map[string]*template.Template)
	for _, p := range pages {
		t := template.Must(template.Must(layout.Clone()).ParseFiles(p))
		// Extract filename as the template key
		name := p[len("templates/"):]
		pageTemplates[name] = t
	}
}

func renderTemplate(w http.ResponseWriter, name string, data any) {
	t, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		log.Printf("Template %s not found", name)
		return
	}
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("Template error rendering %s: %v", name, err)
	}
}

func (a *appHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	token := getSessionToken(r)
	if token != "" {
		if role, ok := getSession(a.db, token); ok {
			if role == "admin" {
				http.Redirect(w, r, "/admin", http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, "/calendar", http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *appHandler) handleGuestLogin(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w,"guest_login.html", nil)
}

func (a *appHandler) handleGuestLoginPost(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if password != a.cfg.GuestPassword {
		renderTemplate(w,"guest_login.html", map[string]string{"Error": "Invalid password"})
		return
	}

	token, err := createSession(a.db, "guest")
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)
	http.Redirect(w, r, "/calendar", http.StatusSeeOther)
}

type CalendarDay struct {
	Date             string
	Day              int
	Blocked          bool
	Past             bool
	JesseAvailable   bool
	AllisonAvailable bool
}

type CalendarData struct {
	Year       int
	Month      time.Month
	MonthStr   string
	Days       []CalendarDay
	PadBefore  int
	PrevMonth  string
	NextMonth  string
	CheckIn    string
	Error      string
}

func (a *appHandler) handleCalendar(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year, month := now.Year(), now.Month()

	if m := r.URL.Query().Get("month"); m != "" {
		t, err := time.Parse("2006-01", m)
		if err == nil {
			year, month = t.Year(), t.Month()
		}
	}

	firstDay := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)
	daysInMonth := lastDay.Day()
	padBefore := int(firstDay.Weekday())

	monthStart := firstDay.Format("2006-01-02")
	monthEnd := lastDay.Format("2006-01-02")

	bookedDates, err := getBookedDates(a.db, monthStart, monthEnd)
	if err != nil {
		log.Printf("Error getting booked dates: %v", err)
		bookedDates = make(map[string]bool)
	}

	blockedDates, err := getGoogleBlockedDates(a.calService, a.cfg.GoogleLifeCalendarID, firstDay)
	if err != nil {
		log.Printf("Error getting Google Calendar dates: %v", err)
	}

	lifeAvail, err := getLifeCalendarAvailability(a.calService, a.cfg.GoogleLifeCalendarID, firstDay)
	if err != nil {
		log.Printf("Error getting Life Calendar availability: %v", err)
	}

	today := now.Format("2006-01-02")
	var days []CalendarDay
	for d := 1; d <= daysInMonth; d++ {
		date := time.Date(year, month, d, 0, 0, 0, 0, time.Local)
		dateStr := date.Format("2006-01-02")

		jesseAvail := true
		allisonAvail := true
		if ha, ok := lifeAvail[dateStr]; ok {
			jesseAvail = !ha.JesseAway
			allisonAvail = !ha.AllisonAway
		}

		bothAway := !jesseAvail && !allisonAvail
		blocked := bookedDates[dateStr] || blockedDates[dateStr] || bothAway
		past := dateStr < today

		days = append(days, CalendarDay{
			Date:             dateStr,
			Day:              d,
			Blocked:          blocked,
			Past:             past,
			JesseAvailable:   jesseAvail,
			AllisonAvailable: allisonAvail,
		})
	}

	prev := firstDay.AddDate(0, -1, 0)
	next := firstDay.AddDate(0, 1, 0)

	selectedCheckIn := r.URL.Query().Get("check_in")

	data := CalendarData{
		Year:      year,
		Month:     month,
		MonthStr:  fmt.Sprintf("%d-%02d", year, month),
		Days:      days,
		PadBefore: padBefore,
		PrevMonth: fmt.Sprintf("%d-%02d", prev.Year(), prev.Month()),
		NextMonth: fmt.Sprintf("%d-%02d", next.Year(), next.Month()),
		CheckIn:   selectedCheckIn,
	}

	renderTemplate(w,"calendar.html", data)
}

func (a *appHandler) handleBookPost(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("guest_name"))
	email := strings.TrimSpace(r.FormValue("guest_email"))
	message := strings.TrimSpace(r.FormValue("message"))
	checkIn := r.FormValue("check_in")
	checkOut := r.FormValue("check_out")

	if name == "" || email == "" || checkIn == "" || checkOut == "" {
		renderTemplate(w,"booking_form.html", map[string]any{
			"Error":    "Please fill in all required fields",
			"CheckIn":  checkIn,
			"CheckOut": checkOut,
		})
		return
	}

	if checkIn > checkOut {
		renderTemplate(w,"booking_form.html", map[string]any{
			"Error":    "Check-out must be after check-in",
			"CheckIn":  checkIn,
			"CheckOut": checkOut,
		})
		return
	}

	// Validate no blocked dates in the requested range
	if err := a.validateNoBlockedDates(checkIn, checkOut); err != nil {
		renderTemplate(w,"booking_form.html", map[string]any{
			"Error":    err.Error(),
			"CheckIn":  checkIn,
			"CheckOut": checkOut,
		})
		return
	}

	b := &Booking{
		GuestName:  name,
		GuestEmail: email,
		Message:    message,
		CheckIn:    checkIn,
		CheckOut:   checkOut,
	}
	if err := insertBooking(a.db, b); err != nil {
		log.Printf("Error inserting booking: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	go notifyAdminNewBooking(a.cfg, b)

	renderTemplate(w,"booking_confirm.html", b)
}

func (a *appHandler) handleBookingForm(w http.ResponseWriter, r *http.Request) {
	checkIn := r.URL.Query().Get("check_in")
	checkOut := r.URL.Query().Get("check_out")
	renderTemplate(w,"booking_form.html", map[string]any{
		"CheckIn":  checkIn,
		"CheckOut": checkOut,
	})
}

func (a *appHandler) handleBookingStatus(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uuid")
	b, err := getBookingByUUID(a.db, uid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "booking_status.html", b)
}

func (a *appHandler) handleCancelBooking(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uuid")
	if err := cancelBooking(a.db, uid); err != nil {
		http.Error(w, "Unable to cancel booking", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/booking/"+uid, http.StatusSeeOther)
}

func (a *appHandler) validateNoBlockedDates(checkIn, checkOut string) error {
	start, err := time.Parse("2006-01-02", checkIn)
	if err != nil {
		return fmt.Errorf("Invalid check-in date")
	}
	end, err := time.Parse("2006-01-02", checkOut)
	if err != nil {
		return fmt.Errorf("Invalid check-out date")
	}

	bookedDates, err := getBookedDates(a.db, checkIn, checkOut)
	if err != nil {
		return fmt.Errorf("Unable to verify availability")
	}

	// Collect Google Calendar blocked dates and host availability for each month in the range
	googleBlocked := make(map[string]bool)
	lifeAvail := make(map[string]HostAvailability)
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.Local); !m.After(end); m = m.AddDate(0, 1, 0) {
		dates, err := getGoogleBlockedDates(a.calService, a.cfg.GoogleLifeCalendarID, m)
		if err != nil {
			log.Printf("Error checking Google Calendar for %s: %v", m.Format("2006-01"), err)
		}
		for k, v := range dates {
			googleBlocked[k] = v
		}

		avail, err := getLifeCalendarAvailability(a.calService, a.cfg.GoogleLifeCalendarID, m)
		if err != nil {
			log.Printf("Error checking life calendar for %s: %v", m.Format("2006-01"), err)
		}
		for k, v := range avail {
			lifeAvail[k] = v
		}
	}

	// Check each day in the range using the same logic as the calendar view
	today := time.Now().Format("2006-01-02")
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		if dateStr < today {
			return fmt.Errorf("Some dates in your requested stay are in the past. Please choose different dates.")
		}
		bothAway := false
		if ha, ok := lifeAvail[dateStr]; ok {
			bothAway = ha.JesseAway && ha.AllisonAway
		}
		if bookedDates[dateStr] || googleBlocked[dateStr] || bothAway {
			return fmt.Errorf("Some dates in your requested stay are unavailable. Please choose different dates.")
		}
	}

	return nil
}

func (a *appHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := getSessionToken(r)
	if token != "" {
		deleteSession(a.db, token)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
