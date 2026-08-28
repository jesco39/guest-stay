package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func initDB(path string) (*sql.DB, error) {
	// busy_timeout makes a second writer wait for the lock instead of failing outright.
	// Approval races on the conditional UPDATE in claimBookingForApproval, so without it
	// a double-clicked Approve returns SQLITE_BUSY and a 500 rather than one clean win.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS bookings (
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
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			role TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);
	`)
	if err != nil {
		return nil, err
	}

	// Migration: add uuid column
	_, err = db.Exec(`ALTER TABLE bookings ADD COLUMN uuid TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return nil, err
	}

	// Migration: add booking_type column
	_, err = db.Exec(`ALTER TABLE bookings ADD COLUMN booking_type TEXT NOT NULL DEFAULT 'regular'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return nil, err
	}

	// Backfill existing rows that have no UUID. Collect the ids first: updating while
	// the cursor is still open leaves the rows unwritten.
	rows, err := db.Query("SELECT id FROM bookings WHERE uuid IS NULL OR uuid = ''")
	if err != nil {
		return nil, err
	}
	var missingUUID []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		missingUUID = append(missingUUID, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for _, id := range missingUUID {
		if _, err := db.Exec("UPDATE bookings SET uuid = ? WHERE id = ?", uuid.New().String(), id); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// Booking types. A stay is cat_sitting when its date range overlaps at least one
// day on which both hosts are away.
const (
	bookingTypeRegular    = "regular"
	bookingTypeCatSitting = "cat_sitting"
)

type Booking struct {
	ID              int64
	UUID            string
	BookingType     string
	GuestName       string
	GuestEmail      string
	Message         string
	CheckIn         string
	CheckOut        string
	Status          string
	CalendarEventID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func insertBooking(db *sql.DB, b *Booking) error {
	b.UUID = uuid.New().String()
	if b.BookingType == "" {
		b.BookingType = bookingTypeRegular
	}
	res, err := db.Exec(
		`INSERT INTO bookings (guest_name, guest_email, message, check_in, check_out, status, uuid, booking_type)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
		b.GuestName, b.GuestEmail, b.Message, b.CheckIn, b.CheckOut, b.UUID, b.BookingType,
	)
	if err != nil {
		return err
	}
	b.ID, _ = res.LastInsertId()
	return nil
}

// normalizeBookingType maps a scanned booking_type to a known value. Rows that
// predate the column read back as regular.
func normalizeBookingType(v sql.NullString) string {
	if v.Valid && v.String == bookingTypeCatSitting {
		return bookingTypeCatSitting
	}
	return bookingTypeRegular
}

func getBooking(db *sql.DB, id int64) (*Booking, error) {
	b := &Booking{}
	var createdAt, updatedAt string
	var calEventID, uid, bookingType sql.NullString
	err := db.QueryRow(
		`SELECT id, uuid, guest_name, guest_email, message, check_in, check_out, status, calendar_event_id, booking_type, created_at, updated_at
		 FROM bookings WHERE id = ?`, id,
	).Scan(&b.ID, &uid, &b.GuestName, &b.GuestEmail, &b.Message, &b.CheckIn, &b.CheckOut, &b.Status, &calEventID, &bookingType, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	b.UUID = uid.String
	b.CalendarEventID = calEventID.String
	b.BookingType = normalizeBookingType(bookingType)
	b.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	b.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return b, nil
}

func getBookingByUUID(db *sql.DB, uid string) (*Booking, error) {
	b := &Booking{}
	var createdAt, updatedAt string
	var calEventID, bookingType sql.NullString
	err := db.QueryRow(
		`SELECT id, uuid, guest_name, guest_email, message, check_in, check_out, status, calendar_event_id, booking_type, created_at, updated_at
		 FROM bookings WHERE uuid = ?`, uid,
	).Scan(&b.ID, &b.UUID, &b.GuestName, &b.GuestEmail, &b.Message, &b.CheckIn, &b.CheckOut, &b.Status, &calEventID, &bookingType, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	b.CalendarEventID = calEventID.String
	b.BookingType = normalizeBookingType(bookingType)
	b.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	b.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return b, nil
}

func cancelBooking(db *sql.DB, uid string) error {
	res, err := db.Exec(
		`UPDATE bookings SET status = 'cancelled', updated_at = datetime('now')
		 WHERE uuid = ? AND status = 'pending'`, uid,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("booking not found or not cancellable")
	}
	return nil
}

func listBookings(db *sql.DB, status string) ([]Booking, error) {
	query := `SELECT id, uuid, guest_name, guest_email, message, check_in, check_out, status, calendar_event_id, booking_type, created_at, updated_at
		 FROM bookings`
	var args []any
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []Booking
	for rows.Next() {
		var b Booking
		var createdAt, updatedAt string
		var calEventID, uid, bookingType sql.NullString
		if err := rows.Scan(&b.ID, &uid, &b.GuestName, &b.GuestEmail, &b.Message, &b.CheckIn, &b.CheckOut, &b.Status, &calEventID, &bookingType, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		b.UUID = uid.String
		b.CalendarEventID = calEventID.String
		b.BookingType = normalizeBookingType(bookingType)
		b.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		b.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		bookings = append(bookings, b)
	}
	return bookings, nil
}

func updateBookingStatus(db *sql.DB, id int64, status string) error {
	_, err := db.Exec(
		`UPDATE bookings SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id,
	)
	return err
}

// claimBookingForApproval atomically transitions a pending booking to approved and
// reports whether this call is the one that won. The conditional UPDATE is the guard:
// a read-then-write status check leaves a window — wide, because approval does calendar
// round trips — in which a double-clicked Approve button creates a second calendar event,
// orphans the stored event id, and re-derives the type against the booking's own event.
// It also covers denied and cancelled bookings, which are not approvable either.
func claimBookingForApproval(db *sql.DB, id int64) (bool, error) {
	res, err := db.Exec(
		`UPDATE bookings SET status = 'approved', updated_at = datetime('now')
		 WHERE id = ? AND status = 'pending'`, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// clearBookingCalendarEvent drops a stored event id after the event is gone, so a later
// read cannot mistake a deleted event for one that still needs removing.
func clearBookingCalendarEvent(db *sql.DB, id int64) error {
	_, err := db.Exec(
		`UPDATE bookings SET calendar_event_id = '', updated_at = datetime('now') WHERE id = ?`, id,
	)
	return err
}

func updateBookingType(db *sql.DB, id int64, bookingType string) error {
	_, err := db.Exec(
		`UPDATE bookings SET booking_type = ?, updated_at = datetime('now') WHERE id = ?`,
		bookingType, id,
	)
	return err
}

func setBookingCalendarEvent(db *sql.DB, id int64, eventID string) error {
	_, err := db.Exec(
		`UPDATE bookings SET calendar_event_id = ?, updated_at = datetime('now') WHERE id = ?`,
		eventID, id,
	)
	return err
}

func getBookedDates(db *sql.DB, monthStart, monthEnd string) (map[string]bool, error) {
	return getBookedDatesExcluding(db, monthStart, monthEnd, 0)
}

// getBookedDatesExcluding is getBookedDates with one booking left out, so a booking can
// be re-evaluated without its own held dates reading as blocked. excludeID 0 excludes
// nothing, since AUTOINCREMENT ids start at 1.
func getBookedDatesExcluding(db *sql.DB, monthStart, monthEnd string, excludeID int64) (map[string]bool, error) {
	rows, err := db.Query(
		`SELECT check_in, check_out FROM bookings
		 WHERE status = 'approved' AND check_out >= ? AND check_in <= ? AND id != ?`,
		monthStart, monthEnd, excludeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dates := make(map[string]bool)
	for rows.Next() {
		var checkIn, checkOut string
		if err := rows.Scan(&checkIn, &checkOut); err != nil {
			return nil, err
		}
		start, _ := time.Parse("2006-01-02", checkIn)
		end, _ := time.Parse("2006-01-02", checkOut)
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			dates[d.Format("2006-01-02")] = true
		}
	}
	return dates, nil
}
