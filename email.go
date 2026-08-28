package main

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"
)

func sendEmail(cfg *Config, to, subject, body string) error {
	if cfg.SMTPHost == "" || cfg.SMTPUsername == "" {
		log.Printf("SMTP not configured, skipping email to %s: %s", to, subject)
		return nil
	}

	from := cfg.SMTPFrom
	if from == "" {
		from = cfg.SMTPUsername
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
	addr := cfg.SMTPHost + ":" + cfg.SMTPPort

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// groupDateRuns collapses a chronological list of dates into contiguous runs, so a
// stay spanning two separate trips reads as two windows rather than one long span:
// 2026-09-10, 2026-09-11, 2026-09-20 -> ["2026-09-10 to 2026-09-11", "2026-09-20"].
func groupDateRuns(dates []string) []string {
	var runs []string
	for i := 0; i < len(dates); {
		start, err := time.Parse("2006-01-02", dates[i])
		if err != nil {
			// Unparseable dates are passed through rather than dropped.
			runs = append(runs, dates[i])
			i++
			continue
		}

		end, j := start, i+1
		for ; j < len(dates); j++ {
			next, err := time.Parse("2006-01-02", dates[j])
			if err != nil || !next.Equal(end.AddDate(0, 0, 1)) {
				break
			}
			end = next
		}

		if end.Equal(start) {
			runs = append(runs, dates[i])
		} else {
			runs = append(runs, fmt.Sprintf("%s to %s", dates[i], end.Format("2006-01-02")))
		}
		i = j
	}
	return runs
}

// catSittingNote renders the cat-sitting section of a notification email, or an
// empty string for a regular stay.
func catSittingNote(b *Booking, catDates []string) string {
	if b.BookingType != bookingTypeCatSitting {
		return ""
	}
	if len(catDates) == 0 {
		// The stay is on the books as cat sitting but the dates could not be re-read.
		// Say so rather than sending a cat-sitting email with nothing in it.
		return "\nThis stay includes cat sitting while we're away.\n"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\nCat sitting (%d day(s)):\n", len(catDates))
	for _, run := range groupDateRuns(catDates) {
		fmt.Fprintf(&sb, "  - %s\n", run)
	}
	return sb.String()
}

func notifyAdminNewBooking(cfg *Config, b *Booking, catDates []string) {
	if len(cfg.AdminEmails) == 0 {
		return
	}
	subject := fmt.Sprintf("New Guest Stay Request: %s", b.GuestName)
	if b.BookingType == bookingTypeCatSitting {
		subject = fmt.Sprintf("New Cat Sitting Request: %s", b.GuestName)
	}
	body := fmt.Sprintf(`A new booking request has been submitted.

Guest: %s
Email: %s
Check-in: %s
Check-out: %s
%sMessage: %s

Review and approve or deny this request:
%s/admin/login`,
		b.GuestName, b.GuestEmail, b.CheckIn, b.CheckOut, catSittingNote(b, catDates), b.Message, cfg.BaseURL)

	for _, email := range cfg.AdminEmails {
		if err := sendEmail(cfg, email, subject, body); err != nil {
			log.Printf("Error sending admin notification to %s: %v", email, err)
		}
	}
}

func notifyGuestApproved(cfg *Config, b *Booking, catDates []string) {
	subject := "Your Guest Stay Has Been Approved!"
	closing := "We look forward to having you!"
	if b.BookingType == bookingTypeCatSitting {
		subject = "Your Cat Sitting Stay Has Been Approved!"
		closing = "Thank you for looking after the cats while we're away!"
	}
	body := fmt.Sprintf(`Hi %s,

Great news! Your stay has been approved.

Check-in: %s
Check-out: %s
%s
%s

View your booking details:
%s/booking/%s`,
		b.GuestName, b.CheckIn, b.CheckOut, catSittingNote(b, catDates), closing, cfg.BaseURL, b.UUID)

	if err := sendEmail(cfg, b.GuestEmail, subject, body); err != nil {
		log.Printf("Error sending approval email to %s: %v", b.GuestEmail, err)
	}
}

func notifyGuestDenied(cfg *Config, b *Booking) {
	subject := "Guest Stay Request Update"
	body := fmt.Sprintf(`Hi %s,

Unfortunately, your stay request for %s to %s could not be accommodated at this time.

Please feel free to try different dates!

View your booking details:
%s/booking/%s

Best regards`,
		b.GuestName, b.CheckIn, b.CheckOut, cfg.BaseURL, b.UUID)

	if err := sendEmail(cfg, b.GuestEmail, subject, body); err != nil {
		log.Printf("Error sending denial email to %s: %v", b.GuestEmail, err)
	}
}

func notifyGuestCancelled(cfg *Config, b *Booking) {
	subject := "Guest Stay Booking Cancelled"
	body := fmt.Sprintf(`Hi %s,

Your booking for %s to %s has been cancelled.

If you have any questions, please reach out to us. Feel free to book again for different dates!

View your booking details:
%s/booking/%s

Best regards`,
		b.GuestName, b.CheckIn, b.CheckOut, cfg.BaseURL, b.UUID)

	if err := sendEmail(cfg, b.GuestEmail, subject, body); err != nil {
		log.Printf("Error sending cancellation email to %s: %v", b.GuestEmail, err)
	}
}

func smtpConfigured(cfg *Config) bool {
	return cfg.SMTPHost != "" && cfg.SMTPUsername != "" && cfg.SMTPPassword != ""
}
