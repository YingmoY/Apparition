package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/YingmoY/Apparition/internal/server/notify"
)

func (a *App) sendRegisterCodeEmail(toEmail, code string, requestTime time.Time, requestIP string) error {
	content, err := notify.RenderMailContent("verify_register", notify.MailTemplateData{
		AppName:       "Apparition",
		Code:          code,
		ExpireMinutes: emailCodeTTLMinutes,
		RequestIP:     requestIP,
		RequestTime:   requestTime.Format("2006-01-02 15:04:05"),
		UserEmail:     toEmail,
		SupportEmail:  a.cfg.SMTP.FromEmail,
	})
	if err != nil {
		return err
	}

	if !a.cfg.SMTP.Enabled {
		log.Printf("[DEV] SMTP disabled, register code for %s: %s", toEmail, code)
		return nil
	}

	fromEmail := strings.TrimSpace(a.cfg.SMTP.FromEmail)
	if fromEmail == "" {
		return fmt.Errorf("smtp.from_email 未配置")
	}

	message := buildMIMEMessage(a.cfg.SMTP.FromName, fromEmail, toEmail, content.Subject, content.TextBody, content.HTMLBody)
	return sendSMTPMail(a.cfg.SMTP, fromEmail, toEmail, message)
}

func buildMIMEMessage(fromName, fromEmail, toEmail, subject, textBody, htmlBody string) []byte {
	boundary := "apparition-boundary"
	headers := []string{
		fmt.Sprintf("From: %s <%s>", fromName, fromEmail),
		fmt.Sprintf("To: <%s>", toEmail),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s", boundary),
		"",
	}

	body := []string{
		fmt.Sprintf("--%s", boundary),
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		textBody,
		fmt.Sprintf("--%s", boundary),
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		htmlBody,
		fmt.Sprintf("--%s--", boundary),
		"",
	}

	return []byte(strings.Join(append(headers, body...), "\r\n"))
}

func sendSMTPMail(cfg SMTPSection, fromEmail, toEmail string, message []byte) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" || cfg.Port <= 0 {
		return fmt.Errorf("smtp.host 或 smtp.port 配置无效")
	}

	addr := fmt.Sprintf("%s:%d", host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, host)
	mode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))

	switch mode {
	case "ssl":
		return sendSMTPOverTLS(addr, host, auth, fromEmail, toEmail, message)
	case "starttls":
		return sendSMTPWithSTARTTLS(addr, host, auth, fromEmail, toEmail, message)
	default:
		return smtp.SendMail(addr, auth, fromEmail, []string{toEmail}, message)
	}
}

func sendSMTPOverTLS(addr, host string, auth smtp.Auth, fromEmail, toEmail string, message []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(fromEmail); err != nil {
		return err
	}
	if err := client.Rcpt(toEmail); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func sendSMTPWithSTARTTLS(addr, host string, auth smtp.Auth, fromEmail, toEmail string, message []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(fromEmail); err != nil {
		return err
	}
	if err := client.Rcpt(toEmail); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
