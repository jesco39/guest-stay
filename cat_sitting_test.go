package main

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDayState(t *testing.T) {
	const (
		bothAway    = "2026-09-10"
		jesseOnly   = "2026-09-11"
		allisonOnly = "2026-09-12"
		bothHome    = "2026-09-13"
		bookedDay   = "2026-09-14"
		googleDay   = "2026-09-15"
		overlapDay  = "2026-09-16"
	)

	lifeAvail := map[string]HostAvailability{
		bothAway:    {JesseAway: true, AllisonAway: true},
		jesseOnly:   {JesseAway: true},
		allisonOnly: {AllisonAway: true},
		overlapDay:  {JesseAway: true, AllisonAway: true},
	}
	booked := map[string]bool{bookedDay: true, overlapDay: true}
	googleBlocked := map[string]bool{googleDay: true}

	tests := []struct {
		name           string
		date           string
		wantBlocked    bool
		wantCatSitting bool
	}{
		{"both hosts away is bookable cat sitting", bothAway, false, true},
		{"only Jesse away stays a normal visit", jesseOnly, false, false},
		{"only Allison away stays a normal visit", allisonOnly, false, false},
		{"both hosts home stays bookable", bothHome, false, false},
		{"approved booking still blocks", bookedDay, true, false},
		{"non-host calendar event still blocks", googleDay, true, false},
		{"a booked cat-sitting day is blocked", overlapDay, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, catSitting := dayState(tt.date, booked, googleBlocked, lifeAvail)
			if blocked != tt.wantBlocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.wantBlocked)
			}
			if catSitting != tt.wantCatSitting {
				t.Errorf("catSitting = %v, want %v", catSitting, tt.wantCatSitting)
			}
		})
	}
}

func TestIsBookingEvent(t *testing.T) {
	tests := map[string]bool{
		"guest stay: allison":    true,
		"cat sitting: jesse":     true,
		"jesse - tokyo":          false,
		"allison - conference":   false,
		"friends visiting":       false,
		"dinner with guest stay": false,
	}
	for title, want := range tests {
		if got := isBookingEvent(title); got != want {
			t.Errorf("isBookingEvent(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestBookingTypeRoundTrip(t *testing.T) {
	db := newTestDB(t)

	cat := &Booking{GuestName: "Sitter", GuestEmail: "s@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-16", BookingType: bookingTypeCatSitting}
	if err := insertBooking(db, cat); err != nil {
		t.Fatalf("insert cat-sitting booking: %v", err)
	}
	reg := &Booking{GuestName: "Visitor", GuestEmail: "v@example.com", CheckIn: "2026-10-01", CheckOut: "2026-10-03"}
	if err := insertBooking(db, reg); err != nil {
		t.Fatalf("insert regular booking: %v", err)
	}

	if got, err := getBooking(db, cat.ID); err != nil || got.BookingType != bookingTypeCatSitting {
		t.Errorf("getBooking type = %q (err %v), want %q", got.BookingType, err, bookingTypeCatSitting)
	}
	if got, err := getBookingByUUID(db, reg.UUID); err != nil || got.BookingType != bookingTypeRegular {
		t.Errorf("getBookingByUUID type = %q (err %v), want %q", got.BookingType, err, bookingTypeRegular)
	}

	all, err := listBookings(db, "")
	if err != nil {
		t.Fatalf("listBookings: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("listBookings returned %d rows, want 2", len(all))
	}
	for _, b := range all {
		if b.BookingType != bookingTypeRegular && b.BookingType != bookingTypeCatSitting {
			t.Errorf("booking %d has unexpected type %q", b.ID, b.BookingType)
		}
	}
}

// TestMigrationBackfillsLegacyRows covers rows written before booking_type existed:
// they must read back as regular, and re-running initDB must be a no-op.
func TestMigrationBackfillsLegacyRows(t *testing.T) {
	path := t.TempDir() + "/legacy.db"

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE bookings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			guest_name TEXT NOT NULL,
			guest_email TEXT NOT NULL,
			message TEXT,
			check_in TEXT NOT NULL,
			check_out TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			calendar_event_id TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO bookings (guest_name, guest_email, message, check_in, check_out)
		VALUES ('Old Guest', 'old@example.com', '', '2026-01-01', '2026-01-03');
	`)
	if err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	legacy.Close()

	db, err := initDB(path)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	defer db.Close()

	rows, err := listBookings(db, "")
	if err != nil {
		t.Fatalf("listBookings: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].BookingType != bookingTypeRegular {
		t.Errorf("legacy row type = %q, want %q", rows[0].BookingType, bookingTypeRegular)
	}
	if rows[0].UUID == "" {
		t.Error("legacy row was not backfilled with a UUID")
	}

	// Re-running the migration must be idempotent.
	db.Close()
	again, err := initDB(path)
	if err != nil {
		t.Fatalf("initDB second run: %v", err)
	}
	again.Close()
}

// TestTemplatesRender guards the cat-sitting template additions, which renderTemplate
// would otherwise only surface as a log line at runtime.
func TestTemplatesRender(t *testing.T) {
	initTemplates()

	cases := []struct {
		page string
		data any
	}{
		{"booking_form.html", bookingForm{
			CheckIn: "2026-09-08", CheckOut: "2026-09-18",
			GuestName: "Sitter", GuestEmail: "s@example.com", Message: "hello",
			CatSittingDates: []string{"2026-09-10", "2026-09-11"},
		}},
		{"booking_form.html", bookingForm{CheckIn: "2026-10-01", CheckOut: "2026-10-03"}},
		{"admin_dashboard.html", map[string]any{
			"Pending":   []Booking{{GuestName: "Sitter", BookingType: bookingTypeCatSitting}},
			"Approved":  []Booking{{GuestName: "Visitor", BookingType: bookingTypeRegular}},
			"Denied":    []Booking{{GuestName: "Denied Sitter", BookingType: bookingTypeCatSitting}},
			"Cancelled": []Booking{{GuestName: "Cancelled Visitor", BookingType: bookingTypeRegular}},
		}},
		{"calendar.html", CalendarData{AvailabilityUnknown: true, Months: []MonthData{{
			Year:                2026,
			Month:               9,
			AvailabilityUnknown: true,
			Days:                []CalendarDay{{Date: "2026-09-10", Day: 10, CatSitting: true}},
		}}}},
		{"calendar.html", CalendarData{Months: []MonthData{{
			Year:  2026,
			Month: 9,
			Days: []CalendarDay{
				{Date: "2026-09-10", Day: 10, CatSitting: true},
				{Date: "2026-09-11", Day: 11, AllisonAvailable: true},
				{Date: "2026-09-12", Day: 12, Blocked: true},
			},
		}}}},
		{"info.html", nil},
	}

	for _, c := range cases {
		tmpl, ok := pageTemplates[c.page]
		if !ok {
			t.Fatalf("template %s not registered", c.page)
		}
		if err := tmpl.ExecuteTemplate(io.Discard, "layout", c.data); err != nil {
			t.Errorf("rendering %s: %v", c.page, err)
		}
	}
}

func TestParseStayRange(t *testing.T) {
	// rangeAvailability makes two Google Calendar calls per month spanned, and the
	// booking form takes its dates from the query string, so bounds are load-bearing.
	farFuture := time.Now().AddDate(2, 0, 0).Format("2006-01-02")

	tests := []struct {
		name              string
		checkIn, checkOut string
		wantErr           bool
	}{
		{"ordinary stay", "2026-09-08", "2026-09-18", false},
		{"single day", "2026-09-08", "2026-09-08", false},
		{"check-out before check-in", "2026-09-18", "2026-09-08", true},
		{"unparseable check-in", "not-a-date", "2026-09-08", true},
		{"unparseable check-out", "2026-09-08", "not-a-date", true},
		{"longer than the night cap", "2026-09-01", "2027-01-01", true},
		{"unbounded range", "1900-01-01", "2200-01-01", true},
		{"too far ahead", farFuture, farFuture, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseStayRange(tt.checkIn, tt.checkOut)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStayRange(%q, %q) error = %v, wantErr %v", tt.checkIn, tt.checkOut, err, tt.wantErr)
			}
		})
	}
}

func TestGroupDateRuns(t *testing.T) {
	tests := []struct {
		name  string
		dates []string
		want  []string
	}{
		{"empty", nil, nil},
		{"single day", []string{"2026-09-10"}, []string{"2026-09-10"}},
		{"one contiguous run", []string{"2026-09-10", "2026-09-11", "2026-09-12"}, []string{"2026-09-10 to 2026-09-12"}},
		{
			"two separate trips in one stay",
			[]string{"2026-09-10", "2026-09-11", "2026-09-20", "2026-09-21"},
			[]string{"2026-09-10 to 2026-09-11", "2026-09-20 to 2026-09-21"},
		},
		{"run then a lone day", []string{"2026-09-10", "2026-09-11", "2026-09-20"}, []string{"2026-09-10 to 2026-09-11", "2026-09-20"}},
		{"across a month boundary", []string{"2026-09-30", "2026-10-01"}, []string{"2026-09-30 to 2026-10-01"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupDateRuns(tt.dates)
			if len(got) != len(tt.want) {
				t.Fatalf("groupDateRuns(%v) = %v, want %v", tt.dates, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("run %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCatSittingNoteNonContiguous guards the email against reporting two short trips
// as one long span.
func TestCatSittingNoteNonContiguous(t *testing.T) {
	b := &Booking{BookingType: bookingTypeCatSitting}
	note := catSittingNote(b, []string{"2026-09-10", "2026-09-11", "2026-09-20", "2026-09-21"})

	for _, want := range []string{"4 day(s)", "2026-09-10 to 2026-09-11", "2026-09-20 to 2026-09-21"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "2026-09-10 to 2026-09-21") {
		t.Errorf("note collapsed two trips into one span:\n%s", note)
	}

	if got := catSittingNote(&Booking{BookingType: bookingTypeRegular}, []string{"2026-09-10"}); got != "" {
		t.Errorf("regular booking got a cat-sitting note: %q", got)
	}
}

// TestUpdateBookingType covers re-deriving the type at approval, when host travel has
// been added or dropped since the request was submitted.
func TestUpdateBookingType(t *testing.T) {
	db := newTestDB(t)

	b := &Booking{GuestName: "Visitor", GuestEmail: "v@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-12"}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := updateBookingType(db, b.ID, bookingTypeCatSitting); err != nil {
		t.Fatalf("updateBookingType: %v", err)
	}
	got, err := getBooking(db, b.ID)
	if err != nil {
		t.Fatalf("getBooking: %v", err)
	}
	if got.BookingType != bookingTypeCatSitting {
		t.Errorf("type = %q, want %q", got.BookingType, bookingTypeCatSitting)
	}
	if got.Status != "pending" {
		t.Errorf("status = %q, want pending — updateBookingType must not touch status", got.Status)
	}

	if err := updateBookingType(db, b.ID, bookingTypeRegular); err != nil {
		t.Fatalf("updateBookingType back to regular: %v", err)
	}
	if got, _ := getBooking(db, b.ID); got.BookingType != bookingTypeRegular {
		t.Errorf("type = %q, want %q", got.BookingType, bookingTypeRegular)
	}
}

// TestMonthsInRange guards the time-zone regression: start/end come from time.Parse
// (UTC), and a cursor built in time.Local dropped the final month west of UTC whenever
// check-out fell on the 1st — silently skipping that month's availability.
func TestMonthsInRange(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	orig := time.Local
	time.Local = ny
	t.Cleanup(func() { time.Local = orig })

	tests := []struct {
		name              string
		checkIn, checkOut string
		want              []string
	}{
		{"within one month", "2026-09-08", "2026-09-18", []string{"2026-09"}},
		{"check-out on the 1st of the next month", "2026-09-28", "2026-10-01", []string{"2026-09", "2026-10"}},
		{"spanning three months", "2026-09-28", "2026-11-02", []string{"2026-09", "2026-10", "2026-11"}},
		{"across a year boundary", "2026-12-28", "2027-01-01", []string{"2026-12", "2027-01"}},
		{"single day", "2026-09-08", "2026-09-08", []string{"2026-09"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, _ := time.Parse("2006-01-02", tt.checkIn)
			end, _ := time.Parse("2006-01-02", tt.checkOut)

			got := monthsInRange(start, end)
			if len(got) != len(tt.want) {
				t.Fatalf("monthsInRange(%s, %s) = %v, want %v", tt.checkIn, tt.checkOut, formatMonths(got), tt.want)
			}
			for i := range got {
				if got[i].Format("2006-01") != tt.want[i] {
					t.Errorf("month %d = %s, want %s", i, got[i].Format("2006-01"), tt.want[i])
				}
			}
		})
	}
}

func formatMonths(months []time.Time) []string {
	out := make([]string, len(months))
	for i, m := range months {
		out[i] = m.Format("2006-01")
	}
	return out
}

// TestGetBookedDatesExcluding covers re-evaluating an already-approved booking, which
// must not read its own held dates as blocked.
func TestGetBookedDatesExcluding(t *testing.T) {
	db := newTestDB(t)

	b := &Booking{GuestName: "Sitter", GuestEmail: "s@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-12"}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := updateBookingStatus(db, b.ID, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	all, err := getBookedDates(db, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatalf("getBookedDates: %v", err)
	}
	if !all["2026-09-11"] {
		t.Error("approved booking should block its own dates for everyone else")
	}

	excluded, err := getBookedDatesExcluding(db, "2026-09-01", "2026-09-30", b.ID)
	if err != nil {
		t.Fatalf("getBookedDatesExcluding: %v", err)
	}
	if len(excluded) != 0 {
		t.Errorf("booking still blocks itself when excluded: %v", excluded)
	}
}

// TestCatSittingNoteWithoutDates covers the approval path when the dates could not be
// re-read: a booking stored as cat sitting must still say so.
func TestCatSittingNoteWithoutDates(t *testing.T) {
	note := catSittingNote(&Booking{BookingType: bookingTypeCatSitting}, nil)
	if !strings.Contains(note, "cat sitting") {
		t.Errorf("cat-sitting booking with unknown dates lost its note: %q", note)
	}
	if got := catSittingNote(&Booking{BookingType: bookingTypeRegular}, nil); got != "" {
		t.Errorf("regular booking got a note: %q", got)
	}
}

// TestClaimBookingForApproval covers the guard that makes approval a one-shot: only a
// pending booking can be claimed, and only once.
func TestClaimBookingForApproval(t *testing.T) {
	for _, tt := range []struct {
		status string
		want   bool
	}{
		{"pending", true},
		{"approved", false},
		{"denied", false},
		{"cancelled", false},
	} {
		t.Run(tt.status, func(t *testing.T) {
			db := newTestDB(t)
			b := &Booking{GuestName: "G", GuestEmail: "g@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-12"}
			if err := insertBooking(db, b); err != nil {
				t.Fatalf("insert: %v", err)
			}
			if tt.status != "pending" {
				if err := updateBookingStatus(db, b.ID, tt.status); err != nil {
					t.Fatalf("set status: %v", err)
				}
			}

			claimed, err := claimBookingForApproval(db, b.ID)
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if claimed != tt.want {
				t.Errorf("claim on %s booking = %v, want %v", tt.status, claimed, tt.want)
			}

			// A second claim never succeeds, whatever the first one did.
			again, err := claimBookingForApproval(db, b.ID)
			if err != nil {
				t.Fatalf("second claim: %v", err)
			}
			if again {
				t.Error("second claim succeeded; approval is not one-shot")
			}
		})
	}
}

// TestConcurrentApprovalClaims is the reason the guard is a conditional UPDATE rather
// than a status read: approval does calendar round trips, so a read-then-write check
// leaves a wide window in which a double-clicked Approve button is admitted twice.
func TestConcurrentApprovalClaims(t *testing.T) {
	db := newTestDB(t)
	b := &Booking{GuestName: "G", GuestEmail: "g@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-12"}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]bool, racers)
	errs := make([]error, racers)
	start := make(chan struct{})

	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = claimBookingForApproval(db, b.ID)
		}(i)
	}
	close(start)
	wg.Wait()

	claims := 0
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if results[i] {
			claims++
		}
	}
	if claims != 1 {
		t.Errorf("%d concurrent claims succeeded, want exactly 1", claims)
	}
}

// TestHandleApproveRejectsNonPending drives the real handler. A cancelled booking may
// still have its own event on the Life calendar if removal failed, and re-deriving the
// type against that event yields zero cat-sitting days — so approving it would persist a
// cat_sitting -> regular downgrade and orphan the stored event id.
func TestHandleApproveRejectsNonPending(t *testing.T) {
	initTemplates()
	db := newTestDB(t)
	app := &appHandler{db: db, cfg: &Config{}}

	for _, status := range []string{"cancelled", "denied", "approved"} {
		t.Run(status, func(t *testing.T) {
			b := &Booking{
				GuestName: "Sitter", GuestEmail: "s@example.com",
				CheckIn: "2026-09-10", CheckOut: "2026-09-12",
				BookingType: bookingTypeCatSitting,
			}
			if err := insertBooking(db, b); err != nil {
				t.Fatalf("insert: %v", err)
			}
			if err := updateBookingStatus(db, b.ID, status); err != nil {
				t.Fatalf("set status: %v", err)
			}
			if err := setBookingCalendarEvent(db, b.ID, "evt-original"); err != nil {
				t.Fatalf("set event: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/admin/approve/"+strconv.FormatInt(b.ID, 10), nil)
			req.SetPathValue("id", strconv.FormatInt(b.ID, 10))
			rec := httptest.NewRecorder()
			app.handleApprove(rec, req)

			if rec.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
			}

			got, err := getBooking(db, b.ID)
			if err != nil {
				t.Fatalf("getBooking: %v", err)
			}
			if got.Status != status {
				t.Errorf("status = %q, want %q — a %s booking must not be approvable", got.Status, status, status)
			}
			if got.BookingType != bookingTypeCatSitting {
				t.Errorf("type = %q, want %q — approving a %s booking downgraded it", got.BookingType, bookingTypeCatSitting, status)
			}
			if got.CalendarEventID != "evt-original" {
				t.Errorf("event id = %q, want evt-original — the original event was orphaned", got.CalendarEventID)
			}
		})
	}
}

// TestHandleApproveApprovesPending is the positive counterpart: a pending booking is
// approved exactly once, and a second POST changes nothing.
func TestHandleApproveApprovesPending(t *testing.T) {
	initTemplates()
	db := newTestDB(t)
	app := &appHandler{db: db, cfg: &Config{}}

	b := &Booking{
		GuestName: "Sitter", GuestEmail: "s@example.com",
		CheckIn: "2026-09-10", CheckOut: "2026-09-12",
		BookingType: bookingTypeCatSitting,
	}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}

	approve := func() {
		req := httptest.NewRequest(http.MethodPost, "/admin/approve/"+strconv.FormatInt(b.ID, 10), nil)
		req.SetPathValue("id", strconv.FormatInt(b.ID, 10))
		app.handleApprove(httptest.NewRecorder(), req)
	}

	approve()
	first, err := getBooking(db, b.ID)
	if err != nil {
		t.Fatalf("getBooking: %v", err)
	}
	if first.Status != "approved" {
		t.Fatalf("status = %q, want approved", first.Status)
	}

	// The booking must no longer be claimable. Comparing updated_at is not enough:
	// datetime('now') has one-second granularity, so a second successful claim within the
	// same second is invisible and the assertion passes even without the guard.
	claimable, err := claimBookingForApproval(db, b.ID)
	if err != nil {
		t.Fatalf("claim after approval: %v", err)
	}
	if claimable {
		t.Error("booking is still claimable after approval; the claim is not one-shot")
	}

	approve()
	second, err := getBooking(db, b.ID)
	if err != nil {
		t.Fatalf("getBooking: %v", err)
	}
	if second.BookingType != first.BookingType {
		t.Errorf("second approval changed the type: %q -> %q", first.BookingType, second.BookingType)
	}
}

// TestAdminBookingBody asserts the layout of the body that is actually sent, rather than
// re-deriving it in the test.
func TestAdminBookingBody(t *testing.T) {
	cfg := &Config{BaseURL: "https://example.com"}

	cat := adminBookingBody(cfg, &Booking{
		GuestName: "Sitter", GuestEmail: "s@example.com",
		CheckIn: "2026-09-08", CheckOut: "2026-09-18",
		Message: "hello there", BookingType: bookingTypeCatSitting,
	}, []string{"2026-09-10", "2026-09-11", "2026-09-20"})

	if !strings.Contains(cat, "Cat sitting (3 day(s)):") {
		t.Errorf("missing cat-sitting heading:\n%s", cat)
	}
	if !strings.Contains(cat, "  - 2026-09-10 to 2026-09-11") || !strings.Contains(cat, "  - 2026-09-20") {
		t.Errorf("missing contiguous runs:\n%s", cat)
	}
	if !strings.Contains(cat, "\n\nMessage: hello there") {
		t.Errorf("cat-sitting list runs straight into the message:\n%s", cat)
	}

	regular := adminBookingBody(cfg, &Booking{
		GuestName: "Visitor", GuestEmail: "v@example.com",
		CheckIn: "2026-10-01", CheckOut: "2026-10-03",
		Message: "hi", BookingType: bookingTypeRegular,
	}, nil)

	if strings.Contains(regular, "Cat sitting") {
		t.Errorf("regular booking mentions cat sitting:\n%s", regular)
	}
	if !strings.Contains(regular, "Check-out: 2026-10-03\nMessage: hi") {
		t.Errorf("regular booking gained a blank line before the message:\n%s", regular)
	}
}

// TestApproveKeepsTypeWithoutCalendar covers the ambiguity this ticket exists for: with
// no calendar configured the availability read returns nothing, which is not evidence
// that the hosts are home. Persisting it downgrades a cat-sitting booking that is still
// one.
func TestApproveKeepsTypeWithoutCalendar(t *testing.T) {
	initTemplates()
	db := newTestDB(t)
	app := &appHandler{db: db, cfg: &Config{}} // no calService, no calendar id

	b := &Booking{
		GuestName: "Sitter", GuestEmail: "s@example.com",
		CheckIn: "2026-09-10", CheckOut: "2026-09-12",
		BookingType: bookingTypeCatSitting,
	}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/approve/"+strconv.FormatInt(b.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(b.ID, 10))
	app.handleApprove(httptest.NewRecorder(), req)

	got, err := getBooking(db, b.ID)
	if err != nil {
		t.Fatalf("getBooking: %v", err)
	}
	if got.Status != "approved" {
		t.Errorf("status = %q, want approved", got.Status)
	}
	if got.BookingType != bookingTypeCatSitting {
		t.Errorf("type = %q, want %q — an unconfigured calendar downgraded the booking",
			got.BookingType, bookingTypeCatSitting)
	}
}

// TestCancelKeepsEventIDWithoutCalendar covers the other half of the same ambiguity.
// removeBookingFromCalendar returns without deleting when no calendar is configured, so
// treating that as a successful removal drops the only handle on an event that is still
// out there.
func TestCancelKeepsEventIDWithoutCalendar(t *testing.T) {
	initTemplates()
	db := newTestDB(t)
	app := &appHandler{db: db, cfg: &Config{}}

	b := &Booking{GuestName: "G", GuestEmail: "g@example.com", CheckIn: "2026-09-10", CheckOut: "2026-09-12"}
	if err := insertBooking(db, b); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := updateBookingStatus(db, b.ID, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := setBookingCalendarEvent(db, b.ID, "evt-still-out-there"); err != nil {
		t.Fatalf("set event: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/cancel/"+strconv.FormatInt(b.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(b.ID, 10))
	app.handleAdminCancel(httptest.NewRecorder(), req)

	got, err := getBooking(db, b.ID)
	if err != nil {
		t.Fatalf("getBooking: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
	if got.CalendarEventID != "evt-still-out-there" {
		t.Errorf("event id = %q, want it kept — nothing was removed, so the id is the only handle on the event",
			got.CalendarEventID)
	}
}

// TestBookingFlowWorksWithoutCalendar guards the regression this change could easily
// introduce: the app is meant to run with no Google Calendar at all, so an unconfigured
// calendar must not make validateNoBlockedDates fail closed and refuse every booking.
func TestBookingFlowWorksWithoutCalendar(t *testing.T) {
	db := newTestDB(t)
	app := &appHandler{db: db, cfg: &Config{}}

	checkIn := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	checkOut := time.Now().AddDate(0, 0, 10).Format("2006-01-02")

	if err := app.validateNoBlockedDates(checkIn, checkOut); err != nil {
		t.Errorf("booking refused with no calendar configured: %v", err)
	}

	dates, err := app.catSittingDatesForStay(checkIn, checkOut)
	if err != nil {
		t.Errorf("catSittingDatesForStay errored with no calendar configured: %v", err)
	}
	if len(dates) != 0 {
		t.Errorf("cat-sitting dates = %v, want none with no calendar", dates)
	}

	// The calendar view must not raise the outage banner just because there is no
	// calendar, or every page load on such a deployment carries a warning.
	md := app.buildMonthData(2026, 9)
	if md.AvailabilityUnknown {
		t.Error("calendar view flagged an outage when no calendar is configured")
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := initDB(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
