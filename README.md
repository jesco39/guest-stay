# Guest Stay

A self-hosted guest house booking system built with Go. Guests authenticate with a shared password, pick dates from an interactive calendar, and submit booking requests. Property owners approve or deny requests from an admin dashboard.

## Features

- Interactive calendar with date selection for check-in/check-out
- Admin dashboard to approve or deny booking requests
- Google Calendar integration to sync blocked and booked dates (optional)
- Email notifications for new bookings and approval/denial (optional)
- SQLite database with no external database server required
- Session-based auth with HttpOnly cookies (24-hour expiry)

## Requirements

- Go 1.26+

## Setup

1. Create a `.env` file:

```env
# Required
GUEST_PASSWORD=your-guest-password
ADMIN_USERNAME=admin
ADMIN_PASSWORD=your-admin-password

# Optional - Google Calendar integration
GOOGLE_CREDENTIALS_FILE=./credentials.json
GOOGLE_CALENDAR_ID=your-calendar-id@group.calendar.google.com

# Optional - Email notifications
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=you@gmail.com
SMTP_PASSWORD=your-app-password
SMTP_FROM=you@gmail.com
ADMIN_EMAILS=admin1@example.com,admin2@example.com
```

2. (Optional) Place your Google Calendar API `credentials.json` in the project directory.

## Build & Run

```bash
go build -o guest-stay .
./guest-stay
```

Or run directly:

```bash
go run .
```

The server starts on `http://localhost:8080` (override with `PORT` env var).

## Usage

### Guests

1. Visit `/login` and enter the shared guest password
2. Browse the calendar at `/calendar` to see available dates
3. Click two dates to select a check-in and check-out range
4. Fill in the booking form with your name, email, and an optional message
5. Receive a booking ID to check status at `/booking/{id}`

### Admin

1. Visit `/admin/login` and enter admin credentials
2. View all bookings on the dashboard
3. Approve or deny pending requests — guests are notified by email if SMTP is configured

## Architecture

| File | Purpose |
|------|---------|
| `main.go` | Entry point, route definitions, server startup |
| `handlers.go` | Guest-facing HTTP handlers |
| `admin.go` | Admin HTTP handlers |
| `middleware.go` | `requireGuest()` and `requireAdmin()` auth guards |
| `auth.go` | Session token creation and validation |
| `db.go` | SQLite schema and queries (`bookings`, `sessions` tables) |
| `calendar.go` | Google Calendar sync with 5-minute cache |
| `email.go` | SMTP email notifications |
| `config.go` | `.env` file loading and validation |
| `templates/` | Go HTML templates |
| `static/` | CSS and JavaScript |
