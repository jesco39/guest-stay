package main

import (
	"database/sql"
	"io"
	"testing"

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
		{"booking_form.html", map[string]any{
			"CheckIn":         "2026-09-08",
			"CheckOut":        "2026-09-18",
			"CatSittingDates": []string{"2026-09-10", "2026-09-11"},
		}},
		{"booking_form.html", map[string]any{"CheckIn": "2026-10-01", "CheckOut": "2026-10-03"}},
		{"admin_dashboard.html", map[string]any{
			"Pending":   []Booking{{GuestName: "Sitter", BookingType: bookingTypeCatSitting}},
			"Approved":  []Booking{{GuestName: "Visitor", BookingType: bookingTypeRegular}},
			"Denied":    []Booking{},
			"Cancelled": []Booking{},
		}},
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

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := initDB(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
