package main

import (
	"database/sql"
	"time"
)

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
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

	return db, nil
}

type Booking struct {
	ID              int64
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
	res, err := db.Exec(
		`INSERT INTO bookings (guest_name, guest_email, message, check_in, check_out, status)
		 VALUES (?, ?, ?, ?, ?, 'pending')`,
		b.GuestName, b.GuestEmail, b.Message, b.CheckIn, b.CheckOut,
	)
	if err != nil {
		return err
	}
	b.ID, _ = res.LastInsertId()
	return nil
}

func getBooking(db *sql.DB, id int64) (*Booking, error) {
	b := &Booking{}
	var createdAt, updatedAt string
	var calEventID sql.NullString
	err := db.QueryRow(
		`SELECT id, guest_name, guest_email, message, check_in, check_out, status, calendar_event_id, created_at, updated_at
		 FROM bookings WHERE id = ?`, id,
	).Scan(&b.ID, &b.GuestName, &b.GuestEmail, &b.Message, &b.CheckIn, &b.CheckOut, &b.Status, &calEventID, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	b.CalendarEventID = calEventID.String
	b.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	b.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return b, nil
}

func listBookings(db *sql.DB, status string) ([]Booking, error) {
	query := `SELECT id, guest_name, guest_email, message, check_in, check_out, status, calendar_event_id, created_at, updated_at
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
		var calEventID sql.NullString
		if err := rows.Scan(&b.ID, &b.GuestName, &b.GuestEmail, &b.Message, &b.CheckIn, &b.CheckOut, &b.Status, &calEventID, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		b.CalendarEventID = calEventID.String
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

func setBookingCalendarEvent(db *sql.DB, id int64, eventID string) error {
	_, err := db.Exec(
		`UPDATE bookings SET calendar_event_id = ?, updated_at = datetime('now') WHERE id = ?`,
		eventID, id,
	)
	return err
}

func getBookedDates(db *sql.DB, monthStart, monthEnd string) (map[string]bool, error) {
	rows, err := db.Query(
		`SELECT check_in, check_out FROM bookings WHERE status = 'approved' AND check_out >= ? AND check_in <= ?`,
		monthStart, monthEnd,
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
