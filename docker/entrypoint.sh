#!/bin/sh
set -eu

if [ "$(id -u)" = "0" ]; then
  mkdir -p "${DATA_DIR:-/app/hugo-panel/data}" "${HUGO_DIR:-/app/hugo}" "${MEDIA_DIR:-/app/media}"

  if [ "${CHOWN_DATA:-true}" = "true" ]; then
    chown -R hugo:hugo "${DATA_DIR:-/app/hugo-panel/data}"
  fi

  if [ "${CHOWN_HUGO:-true}" = "true" ]; then
    chown -R hugo:hugo "${HUGO_DIR:-/app/hugo}"
  fi

  if [ "${CHOWN_MEDIA:-true}" = "true" ]; then
    chown -R hugo:hugo "${MEDIA_DIR:-/app/media}"
  fi

  exec gosu hugo "$@"
fi

exec "$@"
