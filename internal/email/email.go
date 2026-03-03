package email

import (
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPConfig holds SMTP configuration for sending emails.
type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	To       string
}

// Send sends an email with the given subject and body.
func (c *SMTPConfig) Send(subject, body string) error {
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		c.User, c.To, subject, body)

	auth := smtp.PlainAuth("", c.User, c.Password, c.Host)
	addr := c.Host + ":" + c.Port

	return smtp.SendMail(addr, auth, c.User, []string{c.To}, []byte(msg))
}

// NormalizeNewlines replaces bare \n and \r with \r\n for SMTP compliance.
func NormalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return s
}
