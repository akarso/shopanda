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
load_existing_env "$ENV_FILE"

SHOPANDA_AUTH_JWT_SECRET=${SHOPANDA_AUTH_JWT_SECRET:-$(generate_secret)}
SHOPANDA_SERVER_HOST=${SHOPANDA_SERVER_HOST:-0.0.0.0}
SHOPANDA_SERVER_PORT=${SHOPANDA_SERVER_PORT:-8080}
SHOPANDA_SERVER_PUBLIC_BASE_URL=${SHOPANDA_SERVER_PUBLIC_BASE_URL:-http://localhost:8080}
DATABASE_URL=${DATABASE_URL:-}
SHOPANDA_DATABASE_HOST=${SHOPANDA_DATABASE_HOST:-localhost}
SHOPANDA_DATABASE_PORT=${SHOPANDA_DATABASE_PORT:-5432}
SHOPANDA_DATABASE_USER=${SHOPANDA_DATABASE_USER:-shopanda}
SHOPANDA_DATABASE_PASSWORD=${SHOPANDA_DATABASE_PASSWORD:-changeme}
SHOPANDA_DATABASE_NAME=${SHOPANDA_DATABASE_NAME:-shopanda}
SHOPANDA_DATABASE_SSLMODE=${SHOPANDA_DATABASE_SSLMODE:-disable}
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
SHOPANDA_SEED_ADMIN_PASSWORD=${SHOPANDA_SEED_ADMIN_PASSWORD:-changeme}
SHOPANDA_DEV_MODE=${SHOPANDA_DEV_MODE:-}
SHOPANDA_TEST_DSN=${SHOPANDA_TEST_DSN:-}

printf 'Server configuration\n'
prompt_value SHOPANDA_SERVER_HOST 'Bind host' "$SHOPANDA_SERVER_HOST"
prompt_value SHOPANDA_SERVER_PORT 'Bind port' "$SHOPANDA_SERVER_PORT"
prompt_value SHOPANDA_SERVER_PUBLIC_BASE_URL 'Public base URL' "$SHOPANDA_SERVER_PUBLIC_BASE_URL"

printf '\nDatabase configuration\n'
prompt_value DATABASE_URL 'Full DATABASE_URL (leave empty to use individual fields)' "$DATABASE_URL" true
if [[ -z "$DATABASE_URL" ]]; then
  prompt_value SHOPANDA_DATABASE_HOST 'Database host' "$SHOPANDA_DATABASE_HOST"
  prompt_value SHOPANDA_DATABASE_PORT 'Database port' "$SHOPANDA_DATABASE_PORT"
  prompt_value SHOPANDA_DATABASE_USER 'Database user' "$SHOPANDA_DATABASE_USER"
  prompt_value SHOPANDA_DATABASE_PASSWORD 'Database password' "$SHOPANDA_DATABASE_PASSWORD" true
  prompt_value SHOPANDA_DATABASE_NAME 'Database name' "$SHOPANDA_DATABASE_NAME"
  prompt_choice SHOPANDA_DATABASE_SSLMODE 'Database sslmode' "$SHOPANDA_DATABASE_SSLMODE" disable require verify-ca verify-full
else
  SHOPANDA_DATABASE_HOST=${SHOPANDA_DATABASE_HOST:-localhost}
  SHOPANDA_DATABASE_PORT=${SHOPANDA_DATABASE_PORT:-5432}
  SHOPANDA_DATABASE_USER=${SHOPANDA_DATABASE_USER:-shopanda}
  SHOPANDA_DATABASE_PASSWORD=${SHOPANDA_DATABASE_PASSWORD:-changeme}
  SHOPANDA_DATABASE_NAME=${SHOPANDA_DATABASE_NAME:-shopanda}
  SHOPANDA_DATABASE_SSLMODE=${SHOPANDA_DATABASE_SSLMODE:-disable}
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
prompt_value SHOPANDA_SEED_ADMIN_PASSWORD 'Seed admin password' "$SHOPANDA_SEED_ADMIN_PASSWORD" true
prompt_value SHOPANDA_DEV_MODE 'Development mode flag (optional)' "$SHOPANDA_DEV_MODE"
prompt_value SHOPANDA_TEST_DSN 'Test DSN (optional)' "$SHOPANDA_TEST_DSN"

if ! write_env_file; then
  printf '\nAborted. Existing %s was left unchanged.\n' "$ENV_FILE"
  exit 0
fi

printf '\nWrote %s\n' "$ENV_FILE"
printf 'Next steps:\n'
printf '  1. Review .env\n'
printf '  2. Run ./shopanda setup or ./app setup depending on your binary name\n'
printf '  3. Start the server with ./shopanda serve or ./app serve\n'