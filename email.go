package main

import (
	"fmt"
	"log"
	"net/smtp"
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

func notifyAdminNewBooking(cfg *Config, b *Booking) {
	if len(cfg.AdminEmails) == 0 {
		return
	}
	subject := fmt.Sprintf("New Guest Stay Request: %s", b.GuestName)
	body := fmt.Sprintf(`A new booking request has been submitted.

Guest: %s
Email: %s
Check-in: %s
Check-out: %s
Message: %s

Review and approve or deny this request:
%s/admin/login`,
		b.GuestName, b.GuestEmail, b.CheckIn, b.CheckOut, b.Message, cfg.BaseURL)

	for _, email := range cfg.AdminEmails {
		if err := sendEmail(cfg, email, subject, body); err != nil {
			log.Printf("Error sending admin notification to %s: %v", email, err)
		}
	}
}

func notifyGuestApproved(cfg *Config, b *Booking) {
	subject := "Your Guest Stay Has Been Approved!"
	body := fmt.Sprintf(`Hi %s,

Great news! Your stay has been approved.

Check-in: %s
Check-out: %s

We look forward to having you!

View your booking details:
%s/booking/%s`,
		b.GuestName, b.CheckIn, b.CheckOut, cfg.BaseURL, b.UUID)

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
