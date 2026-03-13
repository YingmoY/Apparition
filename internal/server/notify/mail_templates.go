package notify

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/YingmoY/Apparition/internal/server/assets"
)

type MailTemplateData struct {
	AppName        string
	Code           string
	ExpireMinutes  int
	RequestIP      string
	RequestTime    string
	UserEmail      string
	SupportEmail   string
	RunStatus      string
	RunTime        string
	ErrorMessage   string
	ActionAdvice   string
	Location       string
	TargetFormName string
}

type MailContent struct {
	Subject  string
	TextBody string
	HTMLBody string
}

func RenderMailContent(templateName string, data MailTemplateData) (MailContent, error) {
	subject, err := renderSingleTemplate(templateName+".subject.tmpl", data)
	if err != nil {
		return MailContent{}, err
	}

	textBody, err := renderSingleTemplate(templateName+".text.tmpl", data)
	if err != nil {
		return MailContent{}, err
	}

	htmlBody, err := renderSingleTemplate(templateName+".html.tmpl", data)
	if err != nil {
		return MailContent{}, err
	}

	return MailContent{
		Subject:  strings.TrimSpace(subject),
		TextBody: strings.TrimSpace(textBody),
		HTMLBody: strings.TrimSpace(htmlBody),
	}, nil
}

func renderSingleTemplate(name string, data MailTemplateData) (string, error) {
	path := filepath.ToSlash(filepath.Join("templates", "mail", name))
	tplBytes, err := assets.MailTemplates.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取邮件模板失败 %s: %w", name, err)
	}

	tpl, err := template.New(name).Parse(string(tplBytes))
	if err != nil {
		return "", fmt.Errorf("解析邮件模板失败 %s: %w", name, err)
	}

	var output bytes.Buffer
	if err := tpl.Execute(&output, data); err != nil {
		return "", fmt.Errorf("渲染邮件模板失败 %s: %w", name, err)
	}

	return output.String(), nil
}
