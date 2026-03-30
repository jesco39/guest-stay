package main

import (
	"log"
	"net/http"

	_ "modernc.org/sqlite"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	db, err := initDB("guest-stay.db")
	if err != nil {
		log.Fatalf("Database error: %v", err)
	}
	defer db.Close()

	cleanExpiredSessions(db)
	initTemplates()

	calService, err := initCalendarService(cfg.GoogleCredentialsFile)
	if err != nil {
		log.Printf("Google Calendar not available: %v", err)
		log.Println("Running without Google Calendar integration")
	}

	app := &appHandler{
		db:         db,
		cfg:        cfg,
		calService: calService,
	}

	mux := http.NewServeMux()

	// Static files
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Guest routes
	mux.HandleFunc("GET /{$}", app.handleIndex)
	mux.HandleFunc("GET /login", app.handleGuestLogin)
	mux.HandleFunc("POST /login", app.handleGuestLoginPost)
	mux.HandleFunc("GET /calendar", app.requireGuest(app.handleCalendar))
	mux.HandleFunc("GET /book", app.requireGuest(app.handleBookingForm))
	mux.HandleFunc("POST /book", app.requireGuest(app.handleBookPost))
	mux.HandleFunc("GET /booking/{uuid}", app.requireGuest(app.handleBookingStatus))
	mux.HandleFunc("POST /booking/{uuid}/cancel", app.requireGuest(app.handleCancelBooking))
	mux.HandleFunc("GET /logout", app.handleLogout)

	// Admin routes
	mux.HandleFunc("GET /admin/login", app.handleAdminLogin)
	mux.HandleFunc("POST /admin/login", app.handleAdminLoginPost)
	mux.HandleFunc("GET /admin", app.requireAdmin(app.handleAdminDashboard))
	mux.HandleFunc("POST /admin/approve/{id}", app.requireAdmin(app.handleApprove))
	mux.HandleFunc("POST /admin/deny/{id}", app.requireAdmin(app.handleDeny))
	mux.HandleFunc("POST /admin/cancel/{id}", app.requireAdmin(app.handleAdminCancel))

	// Wrap with request logging
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s (cookie: %v)", r.Method, r.URL.String(), r.Header.Get("Cookie") != "")
		mux.ServeHTTP(w, r)
	})

	log.Printf("Starting server on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, logged); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
