package notify

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/YingmoY/Apparition/internal/server/assets"
)

type MailTemplateData struct {
	AppName       string
	Code          string
	ExpireMinutes int
	RequestIP     string
	RequestTime   string
	UserEmail     string
	SupportEmail  string
}

type MailContent struct {
	Subject  string
	TextBody string
	HTMLBody string
}

func RenderMailContent(templateName string, data MailTemplateData) (MailContent, error) {
	subject, err := renderSingleTemplate(templateName+".subject.tmpl", data)
	if err != nil {
		return MailContent{}, fmt.Errorf("render subject: %w", err)
	}
	text, err := renderSingleTemplate(templateName+".text.tmpl", data)
	if err != nil {
		return MailContent{}, fmt.Errorf("render text: %w", err)
	}
	html, err := renderSingleTemplate(templateName+".html.tmpl", data)
	if err != nil {
		return MailContent{}, fmt.Errorf("render html: %w", err)
	}
	return MailContent{Subject: subject, TextBody: text, HTMLBody: html}, nil
}

func renderSingleTemplate(filename string, data MailTemplateData) (string, error) {
	path := "templates/mail/" + filename
	content, err := assets.MailTemplates.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}
	tmpl, err := template.New(filename).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", filename, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", filename, err)
	}
	return buf.String(), nil
}
