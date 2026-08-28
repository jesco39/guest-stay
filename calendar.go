package main

import (
	"context"
	"errors"
	"strings"
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

type HostAvailability struct {
	JesseAway   bool
	AllisonAway bool
}

type lifeCacheEntry struct {
	availability map[string]HostAvailability
	expires      time.Time
}

type lifeCalendarCache struct {
	mu      sync.Mutex
	entries map[string]lifeCacheEntry
}

var lifeCalCache = &lifeCalendarCache{
	entries: make(map[string]lifeCacheEntry),
}

// errCalendarNotConfigured reports that no Google Calendar is wired up. It is not a
// failure — the app is designed to run without one, and the booking flow treats an
// absent calendar as "nothing is blocked". It exists so that callers which *persist*
// something based on a read — clearing a stored event id, or re-deriving a booking type
// — can tell "there was nothing to read" apart from "the read succeeded and found
// nothing". Conflating those two clears event ids for events that still exist and
// downgrades cat-sitting bookings that are still cat-sitting.
var errCalendarNotConfigured = errors.New("google calendar not configured")

// isBookingEvent reports whether an all-day event on the Life calendar was
// created by this app for an approved booking, rather than being host travel.
// titleLower must already be lowercased.
func isBookingEvent(titleLower string) bool {
	return strings.HasPrefix(titleLower, "guest stay:") || strings.HasPrefix(titleLower, "cat sitting:")
}

// eventEffect is what one all-day event on the Life calendar means for availability.
type eventEffect struct {
	// Blocks reports an existing booking, whose dates are taken. Only this app's own
	// events block — everything else on the Life calendar is host travel, and travel is
	// what makes a day bookable rather than unavailable.
	Blocks bool

	// JesseAway and AllisonAway report which hosts the event takes out of town. Both
	// away is a cat-sitting day; one away is still a regular guest stay with the other
	// host at home.
	JesseAway   bool
	AllisonAway bool
}

// allDayEvent is the part of a Google Calendar all-day event these rules depend on.
// Start is inclusive; End is exclusive, as the API returns it.
type allDayEvent struct {
	Summary string
	Start   string
	End     string
}

// allDayEventsFrom pulls the all-day events out of an API response, dropping timed
// entries, which never affect availability.
func allDayEventsFrom(events *calendar.Events) []allDayEvent {
	var out []allDayEvent
	for _, e := range events.Items {
		if e.Start == nil || e.Start.Date == "" || e.End == nil {
			continue
		}
		out = append(out, allDayEvent{Summary: e.Summary, Start: e.Start.Date, End: e.End.Date})
	}
	return out
}

// eachDate calls fn for every date an all-day event covers, inclusive.
func (e allDayEvent) eachDate(fn func(dateStr string)) {
	start, err := time.Parse("2006-01-02", e.Start)
	if err != nil {
		return
	}
	end, err := time.Parse("2006-01-02", e.End)
	if err != nil {
		return
	}
	end = end.AddDate(0, 0, -1) // end date is exclusive in all-day events
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		fn(d.Format("2006-01-02"))
	}
}

// blockedDatesFrom returns the dates these events make unavailable. Only this app's own
// bookings block: host travel is what makes a day bookable, not unavailable.
func blockedDatesFrom(events []allDayEvent) map[string]bool {
	dates := make(map[string]bool)
	for _, e := range events {
		if !classifyEvent(strings.ToLower(e.Summary)).Blocks {
			continue
		}
		e.eachDate(func(d string) { dates[d] = true })
	}
	return dates
}

// availabilityFrom returns which hosts are away on each date these events cover.
func availabilityFrom(events []allDayEvent) map[string]HostAvailability {
	avail := make(map[string]HostAvailability)
	for _, e := range events {
		eff := classifyEvent(strings.ToLower(e.Summary))
		if eff.Blocks {
			continue
		}
		e.eachDate(func(d string) {
			ha := avail[d]
			if eff.JesseAway {
				ha.JesseAway = true
			}
			if eff.AllisonAway {
				ha.AllisonAway = true
			}
			avail[d] = ha
		})
	}
	return avail
}

// classifyEvent derives every meaning of an event from one place, so the blocked-dates
// read and the host-availability read cannot disagree about the same event.
//
// They previously did, and it made cat sitting unreachable: an event naming neither host
// ("NoLa for Mare & Jason's wedding") was read as both-hosts-away by one function and as
// a generic blocker by the other, so every cat-sitting day was also blocked and blocked
// wins. titleLower must already be lowercased.
func classifyEvent(titleLower string) eventEffect {
	if isBookingEvent(titleLower) {
		return eventEffect{Blocks: true}
	}

	jesse := strings.Contains(titleLower, "jesse")
	allison := strings.Contains(titleLower, "allison")
	if !jesse && !allison {
		// A trip named for where it is going rather than who is going takes them both.
		return eventEffect{JesseAway: true, AllisonAway: true}
	}
	return eventEffect{JesseAway: jesse, AllisonAway: allison}
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
		return nil, errCalendarNotConfigured
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

	dates := blockedDatesFrom(allDayEventsFrom(events))

	calCache.mu.Lock()
	calCache.entries[key] = cacheEntry{dates: dates, expires: time.Now().Add(5 * time.Minute)}
	calCache.mu.Unlock()

	return dates, nil
}

func removeBookingFromCalendar(srv *calendar.Service, calendarID string, b *Booking) error {
	if srv == nil || calendarID == "" {
		// Nothing was removed. If the booking carries an event id, that event is still
		// on the calendar and the id is the only handle on it.
		return errCalendarNotConfigured
	}
	if b.CalendarEventID == "" {
		return nil
	}

	err := srv.Events.Delete(calendarID, b.CalendarEventID).Do()
	if err != nil {
		return err
	}

	// Invalidate cache for affected months
	start, _ := time.Parse("2006-01-02", b.CheckIn)
	checkOut, _ := time.Parse("2006-01-02", b.CheckOut)
	calCache.mu.Lock()
	delete(calCache.entries, start.Format("2006-01"))
	delete(calCache.entries, checkOut.Format("2006-01"))
	calCache.mu.Unlock()

	return nil
}

func addBookingToCalendar(srv *calendar.Service, calendarID string, b *Booking) (string, error) {
	if srv == nil || calendarID == "" {
		return "", errCalendarNotConfigured
	}

	// Check-out date needs +1 day because Google Calendar all-day end dates are exclusive
	checkOut, _ := time.Parse("2006-01-02", b.CheckOut)
	endDate := checkOut.AddDate(0, 0, 1).Format("2006-01-02")

	summary := "Guest Stay: " + b.GuestName
	if b.BookingType == bookingTypeCatSitting {
		summary = "Cat Sitting: " + b.GuestName
	}

	event := &calendar.Event{
		Summary:     summary,
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

func getLifeCalendarAvailability(srv *calendar.Service, calendarID string, month time.Time) (map[string]HostAvailability, error) {
	if srv == nil || calendarID == "" {
		return nil, errCalendarNotConfigured
	}

	key := month.Format("2006-01")

	lifeCalCache.mu.Lock()
	if entry, ok := lifeCalCache.entries[key]; ok && time.Now().Before(entry.expires) {
		lifeCalCache.mu.Unlock()
		return entry.availability, nil
	}
	lifeCalCache.mu.Unlock()

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

	avail := availabilityFrom(allDayEventsFrom(events))

	lifeCalCache.mu.Lock()
	lifeCalCache.entries[key] = lifeCacheEntry{availability: avail, expires: time.Now().Add(5 * time.Minute)}
	lifeCalCache.mu.Unlock()

	return avail, nil
}
