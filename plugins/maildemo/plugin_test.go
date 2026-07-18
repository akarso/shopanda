package maildemo_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/maildemo"
)

func testApp(cfg *config.Config) *plugin.App {
	return &plugin.App{
		Logger: logger.NewWithWriter(io.Discard, "error"),
		Config: cfg,
	}
}

func TestPlugin_Name(t *testing.T) {
	if got := maildemo.New().Name(); got != "maildemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{
		MailDemo: config.MailDemoPluginConfig{Enabled: false},
	}}
	if err := maildemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestPlugin_Init_RegistersMailSender(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{
		MailDemo: config.MailDemoPluginConfig{Enabled: true, SubjectPrefix: "[test]"},
	}}
	app := testApp(cfg)
	if err := maildemo.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, ok := app.MailSender()
	if !ok {
		t.Fatal("MailSender() ok = false")
	}
	sender, ok := v.(mail.Mailer)
	if !ok {
		t.Fatalf("MailSender type = %T", v)
	}
	if err := sender.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Hello",
		Body:    "Body",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestLogMailer_SubjectPrefix(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, "info")
	m := maildemo.NewLogMailer(log, "[demo]")
	if err := m.Send(context.Background(), mail.Message{
		To:      "a@b.com",
		Subject: "Order shipped",
		Body:    "Thanks",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(buf.String(), "[demo] Order shipped") {
		t.Fatalf("log = %q", buf.String())
	}
}

func TestLogMailer_EmptyRecipient(t *testing.T) {
	m := maildemo.NewLogMailer(logger.NewWithWriter(io.Discard, "error"), "")
	if err := m.Send(context.Background(), mail.Message{Subject: "x"}); err == nil {
		t.Fatal("expected error for empty recipient")
	}
}
