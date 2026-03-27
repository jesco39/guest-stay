package main

import (
	"context"
	"log"
	"sync"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type calendarCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	dates   map[string]bool
	expires time.Time
}

var calCache = &calendarCache{
	entries: make(map[string]cacheEntry),
}

func initCalendarService(credentialsFile string) (*calendar.Service, error) {
	ctx := context.Background()
	srv, err := calendar.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, err
	}
	return srv, nil
}

func getGoogleBlockedDates(srv *calendar.Service, calendarID string, month time.Time) (map[string]bool, error) {
	if srv == nil || calendarID == "" {
		return nil, nil
	}

	key := month.Format("2006-01")

	calCache.mu.Lock()
	if entry, ok := calCache.entries[key]; ok && time.Now().Before(entry.expires) {
		calCache.mu.Unlock()
		return entry.dates, nil
	}
	calCache.mu.Unlock()

	firstDay := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, 0)

	events, err := srv.Events.List(calendarID).
		TimeMin(firstDay.Format(time.RFC3339)).
		TimeMax(lastDay.Format(time.RFC3339)).
		SingleEvents(true).
		Do()
	if err != nil {
		return nil, err
	}

	dates := make(map[string]bool)
	for _, event := range events.Items {
		// Only block on all-day events; skip time-based entries
		if event.Start.Date == "" {
			continue
		}
		start, _ := time.Parse("2006-01-02", event.Start.Date)
		end, _ := time.Parse("2006-01-02", event.End.Date)
		end = end.AddDate(0, 0, -1) // end date is exclusive in all-day events
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			dates[d.Format("2006-01-02")] = true
		}
	}

	calCache.mu.Lock()
	calCache.entries[key] = cacheEntry{dates: dates, expires: time.Now().Add(5 * time.Minute)}
	calCache.mu.Unlock()

	return dates, nil
}

func addBookingToCalendar(srv *calendar.Service, calendarID string, b *Booking) (string, error) {
	if srv == nil || calendarID == "" {
		log.Println("Google Calendar not configured, skipping event creation")
		return "", nil
	}

	// Check-out date needs +1 day because Google Calendar all-day end dates are exclusive
	checkOut, _ := time.Parse("2006-01-02", b.CheckOut)
	endDate := checkOut.AddDate(0, 0, 1).Format("2006-01-02")

	event := &calendar.Event{
		Summary:     "Guest Stay: " + b.GuestName,
		Description: b.Message,
		Start:       &calendar.EventDateTime{Date: b.CheckIn},
		End:         &calendar.EventDateTime{Date: endDate},
	}

	created, err := srv.Events.Insert(calendarID, event).Do()
	if err != nil {
		return "", err
	}

	// Invalidate cache for affected months
	start, _ := time.Parse("2006-01-02", b.CheckIn)
	calCache.mu.Lock()
	delete(calCache.entries, start.Format("2006-01"))
	delete(calCache.entries, checkOut.Format("2006-01"))
	calCache.mu.Unlock()

	return created.Id, nil
}
