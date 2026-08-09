#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE="$SCRIPT_DIR/.env"

KNOWN_ENV_KEYS=(
  SHOPANDA_SERVER_HOST
  SHOPANDA_SERVER_PORT
  SHOPANDA_SERVER_PUBLIC_BASE_URL
  DATABASE_URL
  SHOPANDA_DATABASE_HOST
  SHOPANDA_DATABASE_PORT
  SHOPANDA_DATABASE_USER
  SHOPANDA_DATABASE_PASSWORD
  SHOPANDA_DATABASE_NAME
  SHOPANDA_DATABASE_SSLMODE
  SHOPANDA_LOG_LEVEL
  SHOPANDA_LOG_FORMAT
  SHOPANDA_AUTH_JWT_SECRET
  SHOPANDA_AUTH_JWT_TTL
  SHOPANDA_MAIL_DRIVER
  SHOPANDA_MAIL_SMTP_HOST
  SHOPANDA_MAIL_SMTP_PORT
  SHOPANDA_MAIL_SMTP_USER
  SHOPANDA_MAIL_SMTP_PASSWORD
  SHOPANDA_MAIL_SMTP_FROM
  SHOPANDA_MEDIA_STORAGE
  SHOPANDA_MEDIA_LOCAL_BASE_PATH
  SHOPANDA_MEDIA_LOCAL_BASE_URL
  SHOPANDA_CACHE_DRIVER
  SHOPANDA_FRONTEND_ENABLED
  SHOPANDA_FRONTEND_MODE
  SHOPANDA_FRONTEND_THEME_PATH
  SHOPANDA_CDN_BASE_URL
  SHOPANDA_WEBHOOKS_SECRET_STRIPE
  SHOPANDA_WEBHOOKS_SECRET_PAYPAL
  SHOPANDA_RATE_LIMIT_ENABLED
  SHOPANDA_RATE_LIMIT_DEFAULT_RATE
  SHOPANDA_RATE_LIMIT_DEFAULT_BURST
  SHOPANDA_SEED_ADMIN_PASSWORD
  SHOPANDA_DEV_EMBED_SCHEDULER
  SHOPANDA_DEV_MODE
  SHOPANDA_TEST_DSN
)

PRESERVED_ENV_LINES=()

trim() {
  local value="$1"
  value="${value#${value%%[![:space:]]*}}"
  value="${value%${value##*[![:space:]]}}"
  printf '%s' "$value"
}

load_existing_env() {
  local source_file="$1"
  [[ -f "$source_file" ]] || return 0

  while IFS= read -r line || [[ -n "$line" ]]; do
    line=$(trim "$line")
    [[ -z "$line" ]] && continue
    [[ "$line" == \#* ]] && continue
    [[ "$line" != *=* ]] && continue

    local key="${line%%=*}"
    local value="${line#*=}"

    key=$(trim "$key")
    value=$(trim "$value")

    if [[ "$value" == '"'*'"' ]] || [[ "$value" == "'"*"'" ]]; then
      value="${value:1:${#value}-2}"
    fi

    printf -v "$key" '%s' "$value"
  done < "$source_file"
}

prompt_value() {
  local var_name="$1"
  local prompt_text="$2"
  local default_value="$3"
  local secret="${4:-false}"
  local prompt_display=""
  local value

  if [[ -n "$default_value" ]]; then
    if [[ "$secret" == "true" ]]; then
      prompt_display=" [hidden]"
    else
      prompt_display=" [$default_value]"
    fi
  fi

  if [[ "$secret" == "true" ]]; then
    read -r -s -p "$prompt_text$prompt_display: " value || true
    printf '\n'
  else
    read -r -p "$prompt_text$prompt_display: " value || true
  fi

  if [[ -z "$value" ]]; then
    value="$default_value"
  fi

  printf -v "$var_name" '%s' "$value"
}

prompt_choice() {
  local var_name="$1"
  local prompt_text="$2"
  local default_value="$3"
  shift 3
  local allowed=("$@")
  local value

  while true; do
    read -r -p "$prompt_text [$default_value]: " value || true
    if [[ -z "$value" ]]; then
      value="$default_value"
    fi

    local option
    for option in "${allowed[@]}"; do
      if [[ "$value" == "$option" ]]; then
        printf -v "$var_name" '%s' "$value"
        return 0
      fi
    done

    printf 'Invalid value. Allowed: %s\n' "${allowed[*]}"
  done
}

# generate_secret emits 64 hex characters (openssl rand -hex 32). The app
# accepts this form as-is for HMAC/MFA key material (see jwt.ParseSecret).
generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return 0
  fi

  if command -v python3 >/dev/null 2>&1; then
    python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
    return 0
  fi

  printf 'error: openssl or python3 must be installed to generate a JWT secret\n' >&2
  return 1
}

# is_local_db_host matches config.isLocalDBHost (compose postgres + loopback).
is_local_db_host() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -d '[]')" in
    localhost|127.0.0.1|::1|postgres) return 0 ;;
    *) return 1 ;;
  esac
}

# refuse_insecure_db_settings returns 1 if password/sslmode violate secure-by-default
# (mirrors internal/platform/config validateSecureDefaults).
refuse_insecure_db_settings() {
  local host="$1"
  local password="$2"
  local sslmode="$3"
  local dev_mode="$4"
  local pw_lc ssl_lc dev_lc
  local is_dev=false
  local local_dev=false

  dev_lc=$(printf '%s' "$dev_mode" | tr '[:upper:]' '[:lower:]')
  case "$dev_lc" in
    1|true|yes) is_dev=true ;;
  esac

  pw_lc=$(printf '%s' "$password" | tr '[:upper:]' '[:lower:]')
  if [[ "$is_dev" != "true" && ( "$pw_lc" == "changeme" || "$pw_lc" == "shopanda" ) ]]; then
    printf '\nError: database password %q is forbidden without SHOPANDA_DEV_MODE=true.\n' "$password" >&2
    printf 'Choose a strong password, or set development mode to 1/true/yes for local-only DX.\n' >&2
    return 1
  fi

  ssl_lc=$(printf '%s' "$sslmode" | tr '[:upper:]' '[:lower:]')
  [[ -z "$ssl_lc" ]] && ssl_lc=disable
  if [[ "$is_dev" == "true" ]] && is_local_db_host "$host"; then
    local_dev=true
  fi
  case "$ssl_lc" in
    require|verify-ca|verify-full) ;;
    *)
      if [[ "$local_dev" != "true" ]]; then
        printf '\nError: database sslmode=%q is not allowed (use require, verify-ca, or verify-full).\n' "$ssl_lc" >&2
        printf 'disable/prefer/allow only when SHOPANDA_DEV_MODE is truthy and host is local (localhost/127.0.0.1/::1/postgres).\n' >&2
        return 1
      fi
      ;;
  esac
  return 0
}

# parse_database_url_security prints host, password, sslmode (one per line) from a
# postgres URL or libpq keyword DSN. Requires python3.
parse_database_url_security() {
  local raw="$1"
  if ! command -v python3 >/dev/null 2>&1; then
    printf 'error: python3 is required to validate DATABASE_URL in the installer\n' >&2
    return 1
  fi
  python3 - "$raw" <<'PY'
import sys
from urllib.parse import urlparse, parse_qs, unquote

raw = sys.argv[1].strip()
host = password = sslmode = ""
lower = raw.lower()
if lower.startswith("postgres://") or lower.startswith("postgresql://"):
    u = urlparse(raw)
    host = u.hostname or ""
    if u.password is not None:
        password = unquote(u.password)
    qs = parse_qs(u.query)
    if "sslmode" in qs and qs["sslmode"]:
        sslmode = qs["sslmode"][0]
else:
    for field in raw.split():
        if "=" not in field:
            continue
        k, v = field.split("=", 1)
        k = k.strip().lower()
        v = v.strip().strip("\"'")
        if k in ("host", "hostaddr"):
            host = v
        elif k == "password":
            password = v
        elif k == "sslmode":
            sslmode = v
print(host)
print(password)
print(sslmode)
PY
}

is_known_env_key() {
  local candidate="$1"
  local key

  for key in "${KNOWN_ENV_KEYS[@]}"; do
    if [[ "$candidate" == "$key" ]]; then
      return 0
    fi
  done

  return 1
}

parse_env_key() {
  local line="$1"
  local key

  line=$(trim "$line")
  [[ -z "$line" ]] && return 1
  [[ "$line" == \#* ]] && return 1
  [[ "$line" != *=* ]] && return 1

  key=$(trim "${line%%=*}")
  key="${key#export }"
  key=$(trim "$key")
  [[ -z "$key" ]] && return 1

  printf '%s' "$key"
}

collect_preserved_env_lines() {
  PRESERVED_ENV_LINES=()
  [[ -f "$ENV_FILE" ]] || return 0

  while IFS= read -r line || [[ -n "$line" ]]; do
    local key
    key=$(parse_env_key "$line") || continue
    if ! is_known_env_key "$key"; then
      PRESERVED_ENV_LINES+=("$line")
    fi
  done < "$ENV_FILE"
}

quote_env_value() {
  local value="$1"

  if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    printf 'error: multi-line values are not supported in generated .env files\n' >&2
    return 1
  fi

  if [[ "$value" == *'"'* ]]; then
    if [[ "$value" == *"'"* ]]; then
      printf '%s' "$value"
      return 0
    fi
    printf "'%s'" "$value"
    return 0
  fi

  printf '"%s"' "$value"
}

write_env_line() {
  local key="$1"
  local value="$2"
  printf '%s=%s\n' "$key" "$(quote_env_value "$value")"
}

confirm_overwrite() {
  local response

  while true; do
    read -r -p "$ENV_FILE already exists. Overwrite it? [y/N]: " response || true
    response=$(trim "$response")

    case "$response" in
      y|Y|yes|YES)
        return 0
        ;;
      ""|n|N|no|NO)
        return 1
        ;;
      *)
        printf 'Please answer y or n.\n'
        ;;
    esac
  done
}

write_env_file() {
  local tmp_file
  local backup_file=""

  collect_preserved_env_lines

  if [[ -f "$ENV_FILE" ]]; then
    if ! confirm_overwrite; then
      return 1
    fi
  fi

  tmp_file=$(mktemp "${ENV_FILE}.tmp.XXXXXX")

  {
    printf '# =============================================================================\n'
    printf '# Shopanda — Generated by install.sh\n'
    printf '# =============================================================================\n'
    printf '# Review these values before using them in production.\n\n'

    printf '# === Server ===\n'
    write_env_line SHOPANDA_SERVER_HOST "$SHOPANDA_SERVER_HOST"
    write_env_line SHOPANDA_SERVER_PORT "$SHOPANDA_SERVER_PORT"
    write_env_line SHOPANDA_SERVER_PUBLIC_BASE_URL "$SHOPANDA_SERVER_PUBLIC_BASE_URL"
    printf '\n'

    printf '# === Database (PostgreSQL) ===\n'
    write_env_line DATABASE_URL "$DATABASE_URL"
    write_env_line SHOPANDA_DATABASE_HOST "$SHOPANDA_DATABASE_HOST"
    write_env_line SHOPANDA_DATABASE_PORT "$SHOPANDA_DATABASE_PORT"
    write_env_line SHOPANDA_DATABASE_USER "$SHOPANDA_DATABASE_USER"
    write_env_line SHOPANDA_DATABASE_PASSWORD "$SHOPANDA_DATABASE_PASSWORD"
    write_env_line SHOPANDA_DATABASE_NAME "$SHOPANDA_DATABASE_NAME"
    write_env_line SHOPANDA_DATABASE_SSLMODE "$SHOPANDA_DATABASE_SSLMODE"
    printf '\n'

    printf '# === Logging ===\n'
    write_env_line SHOPANDA_LOG_LEVEL "$SHOPANDA_LOG_LEVEL"
    write_env_line SHOPANDA_LOG_FORMAT "$SHOPANDA_LOG_FORMAT"
    printf '\n'

    printf '# === Authentication ===\n'
    write_env_line SHOPANDA_AUTH_JWT_SECRET "$SHOPANDA_AUTH_JWT_SECRET"
    write_env_line SHOPANDA_AUTH_JWT_TTL "$SHOPANDA_AUTH_JWT_TTL"
    printf '\n'

    printf '# === Mail (SMTP) ===\n'
    write_env_line SHOPANDA_MAIL_DRIVER "$SHOPANDA_MAIL_DRIVER"
    write_env_line SHOPANDA_MAIL_SMTP_HOST "$SHOPANDA_MAIL_SMTP_HOST"
    write_env_line SHOPANDA_MAIL_SMTP_PORT "$SHOPANDA_MAIL_SMTP_PORT"
    write_env_line SHOPANDA_MAIL_SMTP_USER "$SHOPANDA_MAIL_SMTP_USER"
    write_env_line SHOPANDA_MAIL_SMTP_PASSWORD "$SHOPANDA_MAIL_SMTP_PASSWORD"
    write_env_line SHOPANDA_MAIL_SMTP_FROM "$SHOPANDA_MAIL_SMTP_FROM"
    printf '\n'

    printf '# === Media Storage ===\n'
    write_env_line SHOPANDA_MEDIA_STORAGE "$SHOPANDA_MEDIA_STORAGE"
    write_env_line SHOPANDA_MEDIA_LOCAL_BASE_PATH "$SHOPANDA_MEDIA_LOCAL_BASE_PATH"
    write_env_line SHOPANDA_MEDIA_LOCAL_BASE_URL "$SHOPANDA_MEDIA_LOCAL_BASE_URL"
    printf '\n'

    printf '# === Cache ===\n'
    write_env_line SHOPANDA_CACHE_DRIVER "$SHOPANDA_CACHE_DRIVER"
    printf '\n'

    printf '# === Frontend ===\n'
    write_env_line SHOPANDA_FRONTEND_ENABLED "$SHOPANDA_FRONTEND_ENABLED"
    write_env_line SHOPANDA_FRONTEND_MODE "$SHOPANDA_FRONTEND_MODE"
    write_env_line SHOPANDA_FRONTEND_THEME_PATH "$SHOPANDA_FRONTEND_THEME_PATH"
    printf '\n'

    printf '# === CDN ===\n'
    write_env_line SHOPANDA_CDN_BASE_URL "$SHOPANDA_CDN_BASE_URL"
    printf '\n'

    printf '# === Webhooks ===\n'
    write_env_line SHOPANDA_WEBHOOKS_SECRET_STRIPE "$SHOPANDA_WEBHOOKS_SECRET_STRIPE"
    write_env_line SHOPANDA_WEBHOOKS_SECRET_PAYPAL "$SHOPANDA_WEBHOOKS_SECRET_PAYPAL"
    printf '\n'

    printf '# === Rate Limiting ===\n'
    write_env_line SHOPANDA_RATE_LIMIT_ENABLED "$SHOPANDA_RATE_LIMIT_ENABLED"
    write_env_line SHOPANDA_RATE_LIMIT_DEFAULT_RATE "$SHOPANDA_RATE_LIMIT_DEFAULT_RATE"
    write_env_line SHOPANDA_RATE_LIMIT_DEFAULT_BURST "$SHOPANDA_RATE_LIMIT_DEFAULT_BURST"
    printf '\n'

    printf '# === Seeding ===\n'
    write_env_line SHOPANDA_SEED_ADMIN_PASSWORD "$SHOPANDA_SEED_ADMIN_PASSWORD"
    printf '\n'

    printf '# === Development ===\n'
    write_env_line SHOPANDA_DEV_EMBED_SCHEDULER "$SHOPANDA_DEV_EMBED_SCHEDULER"
    write_env_line SHOPANDA_DEV_MODE "$SHOPANDA_DEV_MODE"
    printf '\n'

    printf '# === Testing ===\n'
    write_env_line SHOPANDA_TEST_DSN "$SHOPANDA_TEST_DSN"

    if ((${#PRESERVED_ENV_LINES[@]} > 0)); then
      printf '\n# === Preserved custom variables from previous .env ===\n'
      printf '%s\n' "${PRESERVED_ENV_LINES[@]}"
    fi
  } > "$tmp_file"

  if [[ -f "$ENV_FILE" ]]; then
    backup_file="${ENV_FILE}.bak-$(date +%Y%m%d%H%M%S)-$$"
    cp "$ENV_FILE" "$backup_file"
  fi

  mv "$tmp_file" "$ENV_FILE"

  if [[ -n "$backup_file" ]]; then
    printf 'Backed up existing env file to %s\n' "$backup_file"
  fi
}

printf 'Shopanda interactive installer\n\n'

load_existing_env "$SCRIPT_DIR/.env.example"
# .env.example may document local-dev samples (changeme / sslmode=disable). Clear those
# so installer defaults (empty password → generate, sslmode=require) apply on first run.
# A real .env loaded next can still restore intentional values.
case "$(printf '%s' "${SHOPANDA_DATABASE_PASSWORD:-}" | tr '[:upper:]' '[:lower:]')" in
  changeme|shopanda) SHOPANDA_DATABASE_PASSWORD= ;;
esac
case "$(printf '%s' "${SHOPANDA_DATABASE_SSLMODE:-}" | tr '[:upper:]' '[:lower:]')" in
  disable|prefer|allow|"") SHOPANDA_DATABASE_SSLMODE=require ;;
esac

load_existing_env "$ENV_FILE"

SHOPANDA_AUTH_JWT_SECRET=${SHOPANDA_AUTH_JWT_SECRET:-$(generate_secret)}
SHOPANDA_SERVER_HOST=${SHOPANDA_SERVER_HOST:-0.0.0.0}
SHOPANDA_SERVER_PORT=${SHOPANDA_SERVER_PORT:-8080}
SHOPANDA_SERVER_PUBLIC_BASE_URL=${SHOPANDA_SERVER_PUBLIC_BASE_URL:-http://localhost:8080}
DATABASE_URL=${DATABASE_URL:-}
SHOPANDA_DATABASE_HOST=${SHOPANDA_DATABASE_HOST:-localhost}
SHOPANDA_DATABASE_PORT=${SHOPANDA_DATABASE_PORT:-5432}
SHOPANDA_DATABASE_USER=${SHOPANDA_DATABASE_USER:-shopanda}
# Empty default — installer generates a strong password if the operator leaves it blank.
SHOPANDA_DATABASE_PASSWORD=${SHOPANDA_DATABASE_PASSWORD:-}
SHOPANDA_DATABASE_NAME=${SHOPANDA_DATABASE_NAME:-shopanda}
SHOPANDA_DATABASE_SSLMODE=${SHOPANDA_DATABASE_SSLMODE:-require}
# Production-safe: leave empty unless the operator opts into local relaxations.
SHOPANDA_DEV_MODE=${SHOPANDA_DEV_MODE:-}
SHOPANDA_LOG_LEVEL=${SHOPANDA_LOG_LEVEL:-info}
SHOPANDA_LOG_FORMAT=${SHOPANDA_LOG_FORMAT:-json}
SHOPANDA_AUTH_JWT_TTL=${SHOPANDA_AUTH_JWT_TTL:-24h}
SHOPANDA_MAIL_DRIVER=${SHOPANDA_MAIL_DRIVER:-smtp}
SHOPANDA_MAIL_SMTP_HOST=${SHOPANDA_MAIL_SMTP_HOST:-localhost}
SHOPANDA_MAIL_SMTP_PORT=${SHOPANDA_MAIL_SMTP_PORT:-587}
SHOPANDA_MAIL_SMTP_USER=${SHOPANDA_MAIL_SMTP_USER:-}
SHOPANDA_MAIL_SMTP_PASSWORD=${SHOPANDA_MAIL_SMTP_PASSWORD:-}
SHOPANDA_MAIL_SMTP_FROM=${SHOPANDA_MAIL_SMTP_FROM:-noreply@example.com}
SHOPANDA_MEDIA_STORAGE=${SHOPANDA_MEDIA_STORAGE:-local}
SHOPANDA_MEDIA_LOCAL_BASE_PATH=${SHOPANDA_MEDIA_LOCAL_BASE_PATH:-./public/media}
SHOPANDA_MEDIA_LOCAL_BASE_URL=${SHOPANDA_MEDIA_LOCAL_BASE_URL:-/media}
SHOPANDA_CACHE_DRIVER=${SHOPANDA_CACHE_DRIVER:-postgres}
SHOPANDA_FRONTEND_ENABLED=${SHOPANDA_FRONTEND_ENABLED:-false}
SHOPANDA_FRONTEND_MODE=${SHOPANDA_FRONTEND_MODE:-ssr}
SHOPANDA_FRONTEND_THEME_PATH=${SHOPANDA_FRONTEND_THEME_PATH:-themes/default}
SHOPANDA_CDN_BASE_URL=${SHOPANDA_CDN_BASE_URL:-}
SHOPANDA_WEBHOOKS_SECRET_STRIPE=${SHOPANDA_WEBHOOKS_SECRET_STRIPE:-}
SHOPANDA_WEBHOOKS_SECRET_PAYPAL=${SHOPANDA_WEBHOOKS_SECRET_PAYPAL:-}
SHOPANDA_RATE_LIMIT_ENABLED=${SHOPANDA_RATE_LIMIT_ENABLED:-false}
SHOPANDA_RATE_LIMIT_DEFAULT_RATE=${SHOPANDA_RATE_LIMIT_DEFAULT_RATE:-10}
SHOPANDA_RATE_LIMIT_DEFAULT_BURST=${SHOPANDA_RATE_LIMIT_DEFAULT_BURST:-20}
SHOPANDA_SEED_ADMIN_PASSWORD=${SHOPANDA_SEED_ADMIN_PASSWORD:-}
SHOPANDA_DEV_EMBED_SCHEDULER=${SHOPANDA_DEV_EMBED_SCHEDULER:-true}
SHOPANDA_TEST_DSN=${SHOPANDA_TEST_DSN:-}

printf 'Server configuration\n'
prompt_value SHOPANDA_SERVER_HOST 'Bind host' "$SHOPANDA_SERVER_HOST"
prompt_value SHOPANDA_SERVER_PORT 'Bind port' "$SHOPANDA_SERVER_PORT"
prompt_value SHOPANDA_SERVER_PUBLIC_BASE_URL 'Public base URL' "$SHOPANDA_SERVER_PUBLIC_BASE_URL"

printf '\nDatabase configuration\n'
prompt_value DATABASE_URL 'Full DATABASE_URL (leave empty to use individual fields)' "$DATABASE_URL" true
DB_PASSWORD_GENERATED=false
if [[ -z "$DATABASE_URL" ]]; then
  prompt_value SHOPANDA_DATABASE_HOST 'Database host' "$SHOPANDA_DATABASE_HOST"
  prompt_value SHOPANDA_DATABASE_PORT 'Database port' "$SHOPANDA_DATABASE_PORT"
  prompt_value SHOPANDA_DATABASE_USER 'Database user' "$SHOPANDA_DATABASE_USER"
  prompt_value SHOPANDA_DATABASE_PASSWORD 'Database password (leave blank to auto-generate a strong secret)' "$SHOPANDA_DATABASE_PASSWORD" true
  if [[ -z "$SHOPANDA_DATABASE_PASSWORD" ]]; then
    SHOPANDA_DATABASE_PASSWORD=$(generate_secret)
    DB_PASSWORD_GENERATED=true
  fi
  prompt_value SHOPANDA_DATABASE_NAME 'Database name' "$SHOPANDA_DATABASE_NAME"
  prompt_choice SHOPANDA_DATABASE_SSLMODE 'Database sslmode' "$SHOPANDA_DATABASE_SSLMODE" require verify-ca verify-full disable prefer allow
else
  # DATABASE_URL is authoritative at runtime; clear sample individual fields so refuse
  # checks below do not fail a strong URL because of leftover example password/sslmode.
  SHOPANDA_DATABASE_HOST=${SHOPANDA_DATABASE_HOST:-localhost}
  SHOPANDA_DATABASE_PORT=${SHOPANDA_DATABASE_PORT:-5432}
  SHOPANDA_DATABASE_USER=${SHOPANDA_DATABASE_USER:-shopanda}
  SHOPANDA_DATABASE_PASSWORD=
  SHOPANDA_DATABASE_NAME=${SHOPANDA_DATABASE_NAME:-shopanda}
  SHOPANDA_DATABASE_SSLMODE=require
fi

printf '\nAuthentication and logging\n'
prompt_value SHOPANDA_AUTH_JWT_SECRET 'JWT secret' "$SHOPANDA_AUTH_JWT_SECRET" true
prompt_value SHOPANDA_AUTH_JWT_TTL 'JWT TTL' "$SHOPANDA_AUTH_JWT_TTL"
prompt_choice SHOPANDA_LOG_LEVEL 'Log level' "$SHOPANDA_LOG_LEVEL" debug info warn error
prompt_choice SHOPANDA_LOG_FORMAT 'Log format' "$SHOPANDA_LOG_FORMAT" json text

printf '\nMail configuration\n'
prompt_choice SHOPANDA_MAIL_DRIVER 'Mail driver' "$SHOPANDA_MAIL_DRIVER" smtp
prompt_value SHOPANDA_MAIL_SMTP_HOST 'SMTP host' "$SHOPANDA_MAIL_SMTP_HOST"
prompt_value SHOPANDA_MAIL_SMTP_PORT 'SMTP port' "$SHOPANDA_MAIL_SMTP_PORT"
prompt_value SHOPANDA_MAIL_SMTP_USER 'SMTP user' "$SHOPANDA_MAIL_SMTP_USER"
prompt_value SHOPANDA_MAIL_SMTP_PASSWORD 'SMTP password' "$SHOPANDA_MAIL_SMTP_PASSWORD" true
prompt_value SHOPANDA_MAIL_SMTP_FROM 'SMTP from address' "$SHOPANDA_MAIL_SMTP_FROM"

printf '\nMedia, cache, and frontend\n'
prompt_choice SHOPANDA_MEDIA_STORAGE 'Media storage driver' "$SHOPANDA_MEDIA_STORAGE" local
prompt_value SHOPANDA_MEDIA_LOCAL_BASE_PATH 'Local media path' "$SHOPANDA_MEDIA_LOCAL_BASE_PATH"
prompt_value SHOPANDA_MEDIA_LOCAL_BASE_URL 'Local media base URL' "$SHOPANDA_MEDIA_LOCAL_BASE_URL"
prompt_choice SHOPANDA_CACHE_DRIVER 'Cache driver' "$SHOPANDA_CACHE_DRIVER" postgres
prompt_choice SHOPANDA_FRONTEND_ENABLED 'Enable SSR storefront (true/false)' "$SHOPANDA_FRONTEND_ENABLED" true false
prompt_choice SHOPANDA_FRONTEND_MODE 'Frontend mode' "$SHOPANDA_FRONTEND_MODE" ssr
prompt_value SHOPANDA_FRONTEND_THEME_PATH 'Frontend theme path' "$SHOPANDA_FRONTEND_THEME_PATH"
prompt_value SHOPANDA_CDN_BASE_URL 'CDN base URL (optional)' "$SHOPANDA_CDN_BASE_URL"

printf '\nWebhooks and rate limiting\n'
prompt_value SHOPANDA_WEBHOOKS_SECRET_STRIPE 'Stripe webhook secret (optional)' "$SHOPANDA_WEBHOOKS_SECRET_STRIPE" true
prompt_value SHOPANDA_WEBHOOKS_SECRET_PAYPAL 'PayPal webhook secret (optional)' "$SHOPANDA_WEBHOOKS_SECRET_PAYPAL" true
prompt_choice SHOPANDA_RATE_LIMIT_ENABLED 'Enable rate limiting (true/false)' "$SHOPANDA_RATE_LIMIT_ENABLED" true false
prompt_value SHOPANDA_RATE_LIMIT_DEFAULT_RATE 'Rate limit default rate' "$SHOPANDA_RATE_LIMIT_DEFAULT_RATE"
prompt_value SHOPANDA_RATE_LIMIT_DEFAULT_BURST 'Rate limit default burst' "$SHOPANDA_RATE_LIMIT_DEFAULT_BURST"

printf '\nOptional seeding and development settings\n'
prompt_value SHOPANDA_SEED_ADMIN_PASSWORD 'Seed admin password (leave blank to auto-generate)' "$SHOPANDA_SEED_ADMIN_PASSWORD" true
SEED_ADMIN_PASSWORD_GENERATED=false
if [[ -z "$SHOPANDA_SEED_ADMIN_PASSWORD" ]]; then
  SHOPANDA_SEED_ADMIN_PASSWORD=$(generate_secret)
  SEED_ADMIN_PASSWORD_GENERATED=true
fi
prompt_choice SHOPANDA_DEV_EMBED_SCHEDULER 'Embed scheduler in app dev' "$SHOPANDA_DEV_EMBED_SCHEDULER" true false
prompt_value SHOPANDA_DEV_MODE 'Development mode (1/true/yes for local changeme + sslmode=disable; leave empty for production)' "$SHOPANDA_DEV_MODE"
prompt_value SHOPANDA_TEST_DSN 'Test DSN (optional)' "$SHOPANDA_TEST_DSN"

# Refuse to write an env that fails secure-by-default startup (bare metal).
if [[ -n "$DATABASE_URL" ]]; then
  _db_url_out=$(parse_database_url_security "$DATABASE_URL") || exit 1
  _db_url_host=$(printf '%s\n' "$_db_url_out" | sed -n '1p')
  _db_url_password=$(printf '%s\n' "$_db_url_out" | sed -n '2p')
  _db_url_sslmode=$(printf '%s\n' "$_db_url_out" | sed -n '3p')
  refuse_insecure_db_settings "$_db_url_host" "$_db_url_password" "$_db_url_sslmode" "$SHOPANDA_DEV_MODE" || exit 1
else
  refuse_insecure_db_settings "$SHOPANDA_DATABASE_HOST" "$SHOPANDA_DATABASE_PASSWORD" "$SHOPANDA_DATABASE_SSLMODE" "$SHOPANDA_DEV_MODE" || exit 1
fi

if ! write_env_file; then
  printf '\nAborted. Existing %s was left unchanged.\n' "$ENV_FILE"
  exit 0
fi

printf '\nWrote %s\n' "$ENV_FILE"

if [[ "$DB_PASSWORD_GENERATED" == "true" ]]; then
  printf '\n'
  printf 'Generated database password (shown once — store it securely now):\n\n'
  printf '  %s\n\n' "$SHOPANDA_DATABASE_PASSWORD"
  printf 'Saved in %s as SHOPANDA_DATABASE_PASSWORD.\n' "$ENV_FILE"
fi

if [[ "$SEED_ADMIN_PASSWORD_GENERATED" == "true" ]]; then
  printf '\n'
  printf '=============================================================================\n'
  printf 'Generated admin seed password (shown once — store it securely now):\n\n'
  printf '  %s\n\n' "$SHOPANDA_SEED_ADMIN_PASSWORD"
  printf 'It is saved in %s as SHOPANDA_SEED_ADMIN_PASSWORD and used by `app seed`\n' "$ENV_FILE"
  printf 'to create admin@example.com. Change it after first login.\n'
  printf '=============================================================================\n'
fi

printf 'Next steps:\n'
printf '  1. Review .env\n'
printf '  2. Run ./shopanda setup or start the server and open /setup in your browser\n'
printf '  3. Start the server with ./shopanda dev or ./app dev (HTTP + worker + scheduler)\n'