package assets

import "embed"

//go:embed templates/mail/*
var MailTemplates embed.FS
