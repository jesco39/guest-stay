package main

import (
	"errors"
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
	"add":          func(a, b int) int { return a + b },
	"weekday":      func(d time.Weekday) int { return int(d) },
	"monthName":    func(m time.Month) string { return m.String() },
	"assetVersion": func() string { return Version },
}

func initTemplates() {
	layout := template.Must(template.New("layout").Funcs(funcMap).ParseFiles("templates/layout.html"))

	pages := []string{
		"templates/guest_login.html",
		"templates/calendar.html",
		"templates/booking_form.html",
		"templates/booking_confirm.html",
		"templates/booking_status.html",
		"templates/info.html",
		"templates/admin_login.html",
		"templates/admin_dashboard.html",
	}

	pageTemplates = make(map[string]*template.Template)
	for _, p := range pages {
		var t *template.Template
		if p == "templates/calendar.html" {
			t = template.Must(template.Must(layout.Clone()).ParseFiles(p, "templates/month.html"))
		} else {
			t = template.Must(template.Must(layout.Clone()).ParseFiles(p))
		}
		name := p[len("templates/"):]
		pageTemplates[name] = t
	}

	// Standalone month fragment for the lazy-load endpoint
	pageTemplates["month.html"] = template.Must(template.New("month-frag").Funcs(funcMap).ParseFiles("templates/month.html"))
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
	renderTemplate(w, "guest_login.html", nil)
}

func (a *appHandler) handleGuestLoginPost(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if password != a.cfg.GuestPassword {
		renderTemplate(w, "guest_login.html", map[string]string{"Error": "Invalid password"})
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
	CatSitting       bool
	JesseAvailable   bool
	AllisonAvailable bool
}

type MonthData struct {
	Year      int
	Month     time.Month
	Days      []CalendarDay
	PadBefore int
	// AvailabilityUnknown reports that the calendar read failed, so the days below are
	// not trustworthy. The booking POST fails closed in that case, and rendering the
	// month as freely available with no warning would walk the guest into that error.
	AvailabilityUnknown bool
}

type CalendarData struct {
	Months              []MonthData
	SentinelMonth       string
	Today               string
	AvailabilityUnknown bool
}

func (a *appHandler) buildMonthData(year int, month time.Month) MonthData {
	firstDay := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)
	daysInMonth := lastDay.Day()

	monthStart := firstDay.Format("2006-01-02")
	monthEnd := lastDay.Format("2006-01-02")

	availabilityUnknown := false

	bookedDates, err := getBookedDates(a.db, monthStart, monthEnd)
	if err != nil {
		// Without this the month renders as fully available with approved bookings
		// missing from the grid — the same failure mode as a calendar outage.
		log.Printf("Error getting booked dates for %s: %v", firstDay.Format("2006-01"), err)
		bookedDates = make(map[string]bool)
		availabilityUnknown = true
	}

	// A calendar that was never configured is not an outage — the app is meant to run
	// without one — so it must not raise the warning banner on every page load.
	blockedDates, err := getGoogleBlockedDates(a.calService, a.cfg.GoogleLifeCalendarID, firstDay)
	if err != nil && !errors.Is(err, errCalendarNotConfigured) {
		log.Printf("Error getting Google Calendar dates for %s: %v", firstDay.Format("2006-01"), err)
		availabilityUnknown = true
	}

	lifeAvail, err := getLifeCalendarAvailability(a.calService, a.cfg.GoogleLifeCalendarID, firstDay)
	if err != nil && !errors.Is(err, errCalendarNotConfigured) {
		log.Printf("Error getting Life Calendar availability for %s: %v", firstDay.Format("2006-01"), err)
		availabilityUnknown = true
	}

	today := time.Now().Format("2006-01-02")
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

		blocked, catSitting := dayState(dateStr, bookedDates, blockedDates, lifeAvail)

		days = append(days, CalendarDay{
			Date:             dateStr,
			Day:              d,
			Blocked:          blocked,
			Past:             dateStr < today,
			CatSitting:       catSitting,
			JesseAvailable:   jesseAvail,
			AllisonAvailable: allisonAvail,
		})
	}

	return MonthData{
		Year:                year,
		Month:               month,
		Days:                days,
		PadBefore:           int(firstDay.Weekday()),
		AvailabilityUnknown: availabilityUnknown,
	}
}

func (a *appHandler) handleCalendar(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	months := make([]MonthData, 3)
	for i := range months {
		m := start.AddDate(0, i, 0)
		months[i] = a.buildMonthData(m.Year(), m.Month())
	}

	availabilityUnknown := false
	for _, m := range months {
		if m.AvailabilityUnknown {
			availabilityUnknown = true
			break
		}
	}

	sentinel := start.AddDate(0, 3, 0)
	data := CalendarData{
		Months:              months,
		SentinelMonth:       fmt.Sprintf("%d-%02d", sentinel.Year(), sentinel.Month()),
		Today:               now.Format("2006-01-02"),
		AvailabilityUnknown: availabilityUnknown,
	}

	renderTemplate(w, "calendar.html", data)
}

func (a *appHandler) handleCalendarMonth(w http.ResponseWriter, r *http.Request) {
	t, err := time.Parse("2006-01", r.URL.Query().Get("m"))
	if err != nil {
		http.Error(w, "Invalid month", http.StatusBadRequest)
		return
	}

	md := a.buildMonthData(t.Year(), t.Month())

	tmpl, ok := pageTemplates["month.html"]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "month", md); err != nil {
		log.Printf("Template error rendering month fragment: %v", err)
	}
}

// bookingForm is what the booking page renders: the selected dates, whatever the guest
// has already typed, any error, and the cat-sitting dates that drive the acknowledgement
// checkbox. The typed fields are carried back so a re-render — including the fail-closed
// "try again shortly" branch, which is nobody's fault — does not silently discard them.
type bookingForm struct {
	CheckIn         string
	CheckOut        string
	GuestName       string
	GuestEmail      string
	Message         string
	Error           string
	CatSittingDates []string
}

func renderBookingForm(w http.ResponseWriter, f bookingForm) {
	renderTemplate(w, "booking_form.html", f)
}

func (a *appHandler) handleBookPost(w http.ResponseWriter, r *http.Request) {
	form := bookingForm{
		CheckIn:    r.FormValue("check_in"),
		CheckOut:   r.FormValue("check_out"),
		GuestName:  strings.TrimSpace(r.FormValue("guest_name")),
		GuestEmail: strings.TrimSpace(r.FormValue("guest_email")),
		Message:    strings.TrimSpace(r.FormValue("message")),
	}

	if form.GuestName == "" || form.GuestEmail == "" || form.CheckIn == "" || form.CheckOut == "" {
		// The dates are still usable here, so keep the notice and its checkbox on screen.
		form.CatSittingDates = a.catSittingDatesForRequest(form.CheckIn, form.CheckOut)
		form.Error = "Please fill in all required fields"
		renderBookingForm(w, form)
		return
	}

	if form.CheckIn > form.CheckOut {
		form.Error = "Check-out must be after check-in"
		renderBookingForm(w, form)
		return
	}

	// Validate no blocked dates in the requested range
	if err := a.validateNoBlockedDates(form.CheckIn, form.CheckOut); err != nil {
		// The range is unusable, so there is no notice worth rendering for it.
		form.Error = err.Error()
		renderBookingForm(w, form)
		return
	}

	// Derived from the calendar, never from the form: a guest cannot talk their way out
	// of cat-sitting duty (or into it) by editing the request. Resolved after validation
	// and failing closed, so a transient calendar error cannot quietly route a real
	// cat-sitting stay through as regular with no acknowledgement.
	catDates, err := a.catSittingDatesForStay(form.CheckIn, form.CheckOut)
	if err != nil {
		log.Printf("Error resolving cat-sitting dates for %s..%s: %v", form.CheckIn, form.CheckOut, err)
		form.Error = "We couldn't verify availability just now. Please try again in a few minutes."
		renderBookingForm(w, form)
		return
	}
	form.CatSittingDates = catDates

	if len(catDates) > 0 && r.FormValue("cat_sitting_ack") == "" {
		form.Error = "Please confirm you'll look after the cats on the cat-sitting dates."
		renderBookingForm(w, form)
		return
	}

	b := &Booking{
		GuestName:   form.GuestName,
		GuestEmail:  form.GuestEmail,
		Message:     form.Message,
		CheckIn:     form.CheckIn,
		CheckOut:    form.CheckOut,
		BookingType: bookingTypeRegular,
	}
	if len(catDates) > 0 {
		b.BookingType = bookingTypeCatSitting
	}
	if err := insertBooking(a.db, b); err != nil {
		log.Printf("Error inserting booking: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	go notifyAdminNewBooking(a.cfg, b, catDates)

	renderTemplate(w, "booking_confirm.html", b)
}

func (a *appHandler) handleBookingForm(w http.ResponseWriter, r *http.Request) {
	form := bookingForm{
		CheckIn:  r.URL.Query().Get("check_in"),
		CheckOut: r.URL.Query().Get("check_out"),
	}

	if _, _, err := parseStayRange(form.CheckIn, form.CheckOut); err != nil {
		form.Error = err.Error()
	} else {
		form.CatSittingDates = a.catSittingDatesForRequest(form.CheckIn, form.CheckOut)
	}

	renderBookingForm(w, form)
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

// dayState derives, for a single date, whether it is unbookable and whether it
// falls inside a cat-sitting window. Both hosts being away is what makes a day a
// cat-sitting day: the house needs a sitter, so the day is open for booking rather
// than blocked. Shared by the calendar view and booking validation so the two
// cannot drift.
func dayState(dateStr string, booked, googleBlocked map[string]bool, lifeAvail map[string]HostAvailability) (blocked, catSitting bool) {
	if ha, ok := lifeAvail[dateStr]; ok {
		catSitting = ha.JesseAway && ha.AllisonAway
	}
	return booked[dateStr] || googleBlocked[dateStr], catSitting
}

// Bounds on a requested stay. rangeAvailability fans out into two Google Calendar
// round trips per month spanned, and the booking form takes its dates straight from
// the query string, so an unbounded range would be a free amplification lever.
const (
	maxStayNights  = 90
	maxMonthsAhead = 13

	// Hard backstop on how many months one availability read may span.
	maxMonthsPerRange = 24
)

// parseStayRange validates a requested date range before any calendar work is done
// for it. Returns a guest-facing error message.
func parseStayRange(checkIn, checkOut string) (start, end time.Time, err error) {
	start, err = time.Parse("2006-01-02", checkIn)
	if err != nil {
		return start, end, fmt.Errorf("Invalid check-in date")
	}
	end, err = time.Parse("2006-01-02", checkOut)
	if err != nil {
		return start, end, fmt.Errorf("Invalid check-out date")
	}
	if end.Before(start) {
		return start, end, fmt.Errorf("Check-out must be after check-in")
	}
	if end.Sub(start) > maxStayNights*24*time.Hour {
		return start, end, fmt.Errorf("Stays are limited to %d nights. Please choose a shorter range.", maxStayNights)
	}

	now := time.Now()
	latest := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, maxMonthsAhead, 0)
	if start.After(latest) {
		return start, end, fmt.Errorf("Bookings can only be made up to %d months ahead.", maxMonthsAhead)
	}

	return start, end, nil
}

// availability is what one range read produced, plus whether a Google Calendar actually
// backed it.
type availability struct {
	booked        map[string]bool
	googleBlocked map[string]bool
	life          map[string]HostAvailability

	// calendarRead reports that a calendar was configured and answered. When false the
	// calendar maps are empty because there was nothing to read, not because the hosts
	// are home — anything that persists a classification must not act on that.
	calendarRead bool
}

// rangeAvailability loads booked dates, Google-blocked dates, and host availability
// covering every month spanned by start..end. excludeBookingID drops one booking's own
// held dates from the booked set, so re-reading a booking that is already approved does
// not see itself as a blocker; pass 0 to exclude nothing.
//
// Real calendar errors are returned rather than logged and swallowed: callers decide
// whether a stay is bookable, and an empty availability map is indistinguishable from
// "the hosts are home". An unconfigured calendar is not such an error — it is reported
// through calendarRead instead, so deployments without Google Calendar keep working.
func (a *appHandler) rangeAvailability(checkIn, checkOut string, start, end time.Time, excludeBookingID int64) (availability, error) {
	av := availability{
		googleBlocked: make(map[string]bool),
		life:          make(map[string]HostAvailability),
		calendarRead:  true,
	}

	booked, err := getBookedDatesExcluding(a.db, checkIn, checkOut, excludeBookingID)
	if err != nil {
		return availability{}, err
	}
	av.booked = booked

	// Backstop for any caller that skips the guest-facing caps, so a stored range can
	// never turn into an unbounded loop of calendar round trips.
	months := monthsInRange(start, end)
	if len(months) > maxMonthsPerRange {
		return availability{}, fmt.Errorf("range spans %d months, over the %d month limit", len(months), maxMonthsPerRange)
	}

	// Each read is handled independently rather than skipping the rest of the month on
	// an unconfigured calendar: the two helpers gate on the same condition today, but a
	// `continue` here would silently drop host availability if that ever stopped being
	// true. calendarRead is only ever cleared, never restored, so one unread month marks
	// the whole range as unbacked — the safe direction for a value callers persist.
	for _, m := range months {
		dates, err := getGoogleBlockedDates(a.calService, a.cfg.GoogleLifeCalendarID, m)
		switch {
		case errors.Is(err, errCalendarNotConfigured):
			av.calendarRead = false
		case err != nil:
			return availability{}, fmt.Errorf("checking Google Calendar for %s: %w", m.Format("2006-01"), err)
		default:
			for k, v := range dates {
				av.googleBlocked[k] = v
			}
		}

		avail, err := getLifeCalendarAvailability(a.calService, a.cfg.GoogleLifeCalendarID, m)
		switch {
		case errors.Is(err, errCalendarNotConfigured):
			av.calendarRead = false
		case err != nil:
			return availability{}, fmt.Errorf("checking life calendar for %s: %w", m.Format("2006-01"), err)
		default:
			for k, v := range avail {
				av.life[k] = v
			}
		}
	}

	return av, nil
}

// monthsInRange lists the first day of every calendar month a range touches, inclusive.
// The cursor must share start's location: start and end come from time.Parse (UTC), and
// building it in time.Local drops the final month west of UTC whenever end falls on the
// 1st — silently skipping that month's blocked dates and host availability.
func monthsInRange(start, end time.Time) []time.Time {
	var months []time.Time
	for m := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location()); !m.After(end); m = m.AddDate(0, 1, 0) {
		months = append(months, m)
	}
	return months
}

// catSittingDatesIn resolves the cat-sitting days within an already-validated range,
// in chronological order. A day that is blocked is never part of the stay, so it carries
// no cat duty even when the Life calendar reads as both-hosts-away for it.
func (a *appHandler) catSittingDatesIn(checkIn, checkOut string, start, end time.Time, excludeBookingID int64) ([]string, bool, error) {
	av, err := a.rangeAvailability(checkIn, checkOut, start, end, excludeBookingID)
	if err != nil {
		return nil, false, err
	}

	var dates []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		if blocked, catSitting := dayState(dateStr, av.booked, av.googleBlocked, av.life); catSitting && !blocked {
			dates = append(dates, dateStr)
		}
	}
	return dates, av.calendarRead, nil
}

// catSittingDatesForStay resolves the cat-sitting days for a range a guest is
// requesting, within the guest-facing caps. The error is returned rather than folded
// into an empty result: an empty list waives the acknowledgement requirement and stores
// the stay as regular, so "could not determine" must never reach that decision.
func (a *appHandler) catSittingDatesForStay(checkIn, checkOut string) ([]string, error) {
	start, end, err := parseStayRange(checkIn, checkOut)
	if err != nil {
		return nil, err
	}
	// calendarRead is ignored here: with no calendar there are no known host absences,
	// so a new booking is correctly a regular stay. Nothing is being overwritten.
	dates, _, err := a.catSittingDatesIn(checkIn, checkOut, start, end, 0)
	return dates, err
}

// catSittingDatesForRequest is the display-only form of catSittingDatesForStay, for
// rendering the booking form. It returns nil when the range is invalid or unresolvable,
// which only means the notice is omitted from a page — never that a stay is classified.
// Anything that decides a stay's type must call catSittingDatesForStay and handle the
// error.
func (a *appHandler) catSittingDatesForRequest(checkIn, checkOut string) []string {
	start, end, err := parseStayRange(checkIn, checkOut)
	if err != nil {
		// An unusable range is an ordinary guest mistake, not something to log.
		return nil
	}

	dates, _, err := a.catSittingDatesIn(checkIn, checkOut, start, end, 0)
	if err != nil {
		log.Printf("Error determining cat-sitting dates for %s..%s: %v", checkIn, checkOut, err)
		return nil
	}
	return dates
}

// catSittingDatesForBooking resolves the cat-sitting days for a stored booking. It skips
// the guest-facing caps, which apply to what may be requested rather than to what is
// already on the books, and ignores the booking's own held dates so an already-approved
// booking does not read as blocking itself. The error is returned rather than folded into
// an empty result, and the bool reports whether a calendar actually answered: callers
// persist the classification, and neither "could not determine" nor "there was nothing to
// read" may be mistaken for "no cat-sitting days".
func (a *appHandler) catSittingDatesForBooking(b *Booking) ([]string, bool, error) {
	start, err := time.Parse("2006-01-02", b.CheckIn)
	if err != nil {
		return nil, false, fmt.Errorf("parsing check-in %q: %w", b.CheckIn, err)
	}
	end, err := time.Parse("2006-01-02", b.CheckOut)
	if err != nil {
		return nil, false, fmt.Errorf("parsing check-out %q: %w", b.CheckOut, err)
	}
	if end.Before(start) {
		return nil, false, fmt.Errorf("check-out %s precedes check-in %s", b.CheckOut, b.CheckIn)
	}

	return a.catSittingDatesIn(b.CheckIn, b.CheckOut, start, end, b.ID)
}

func (a *appHandler) validateNoBlockedDates(checkIn, checkOut string) error {
	start, end, err := parseStayRange(checkIn, checkOut)
	if err != nil {
		return err
	}

	av, err := a.rangeAvailability(checkIn, checkOut, start, end, 0)
	if err != nil {
		// Fail closed: without a calendar read there is no way to tell a free day from a
		// blocked one, and accepting the booking risks a double booking.
		log.Printf("Error verifying availability for %s..%s: %v", checkIn, checkOut, err)
		return fmt.Errorf("We couldn't verify availability just now. Please try again in a few minutes.")
	}

	// Check each day in the range using the same logic as the calendar view
	today := time.Now().Format("2006-01-02")
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		if dateStr < today {
			return fmt.Errorf("Some dates in your requested stay are in the past. Please choose different dates.")
		}
		if blocked, _ := dayState(dateStr, av.booked, av.googleBlocked, av.life); blocked {
			return fmt.Errorf("Some dates in your requested stay are unavailable. Please choose different dates.")
		}
	}

	return nil
}

func (a *appHandler) handleInfo(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "info.html", nil)
}

func (a *appHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := getSessionToken(r)
	if token != "" {
		deleteSession(a.db, token)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
