package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/YingmoY/Apparition/internal/server/notify"
)

func (a *App) sendRegisterCodeEmail(to, code string, now time.Time, clientIP string) error {
	if !a.cfg.SMTP.Enabled {
		log.Printf("[DEV] 注册验证码 to=%s code=%s", to, code)
		return nil
	}
	data := notify.MailTemplateData{
		AppName:       "Apparition",
		Code:          code,
		ExpireMinutes: emailCodeTTLMinutes,
		RequestIP:     clientIP,
		RequestTime:   now.Format("2006-01-02 15:04:05 UTC"),
		UserEmail:     to,
		SupportEmail:  a.cfg.SMTP.FromEmail,
	}
	content, err := notify.RenderMailContent("verify_register", data)
	if err != nil {
		return fmt.Errorf("render email template: %w", err)
	}
	msg := buildMIMEMessage(a.cfg.SMTP.FromName, a.cfg.SMTP.FromEmail, to, content.Subject, content.TextBody, content.HTMLBody)
	return a.sendSMTPMail(to, msg)
}

func buildMIMEMessage(fromName, fromEmail, to, subject, textBody, htmlBody string) []byte {
	boundary := fmt.Sprintf("----=_Part_%d", time.Now().UnixNano())
	var b strings.Builder

	b.WriteString(fmt.Sprintf("From: %s\r\n", mime.QEncoding.Encode("utf-8", fromName)+" <"+fromEmail+">"))
	b.WriteString(fmt.Sprintf("To: %s\r\n", to))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject)))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	b.WriteString("\r\n")

	b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(textBody)
	b.WriteString("\r\n")

	b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")

	b.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return []byte(b.String())
}

func (a *App) sendSMTPMail(to string, msg []byte) error {
	cfg := a.cfg.SMTP
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	switch strings.ToLower(cfg.TLSMode) {
	case "ssl", "tls":
		return sendSMTPOverTLS(addr, cfg.Host, cfg.Username, cfg.Password, cfg.FromEmail, to, msg)
	case "starttls":
		return sendSMTPWithSTARTTLS(addr, cfg.Host, cfg.Username, cfg.Password, cfg.FromEmail, to, msg)
	default:
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		return smtp.SendMail(addr, auth, cfg.FromEmail, []string{to}, msg)
	}
}

func sendSMTPOverTLS(addr, host, user, pass, from, to string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}

func sendSMTPWithSTARTTLS(addr, host, user, pass, from, to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}
	if err := client.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	return w.Close()
}
