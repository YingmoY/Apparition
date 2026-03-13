package assets

import "embed"

//go:embed templates/mail/*
var MailTemplates embed.FS

//go:embed web/*
var WebAssets embed.FS
