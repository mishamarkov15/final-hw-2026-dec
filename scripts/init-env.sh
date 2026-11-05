#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ -e .env ]; then
  echo '.env уже существует; файл не изменён.'
  exit 0
fi
umask 077
set -C
{
  echo 'POSTGRES_DB=cashcraft'
  echo 'POSTGRES_USER=cashcraft'
  printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 24)"
  printf 'REDIS_PASSWORD=%s\n' "$(openssl rand -hex 24)"
  echo 'REDIS_DB=0'
  printf 'JWT_SECRET=%s\n' "$(openssl rand -hex 32)"
  echo 'JWT_TTL=1h'
  echo 'PORT=8080'
} > .env
echo 'Создан .env со случайными паролями и JWT_SECRET.'
