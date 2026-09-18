package web

import "embed"

// FS embeds all static web assets (HTML, CSS, JS, locales, etc.)
//
//go:embed index.html style.css app.js i18n.js locales/*
var FS embed.FS
