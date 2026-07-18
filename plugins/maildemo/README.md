# maildemo — reference mail sender plugin

Demonstrates `RegisterMailSender` port replacement from PR-860.

## Enable

```yaml
plugins:
  maildemo:
    enabled: true
    subject_prefix: "[demo]"
```

When enabled, notification email jobs log via `maildemo.send` instead of using core SMTP.

## Behavior

- Implements `mail.Mailer` with a log-backed sender (no external HTTP/API)
- Optional `subject_prefix` prepended to outbound subjects
- Swap `LogMailer` for a SendGrid/Postmark client using the same registration pattern

## See also

- `plugins/taxdemo` — port + pipeline replacement template (PR-853)
- `cmd/api/providers.go` — `resolveMailer` falls back to SMTP when no plugin registers
