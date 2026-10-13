#!/bin/bash
# Restore an encrypted Keygate backup.
#   restore.sh <backup.tar.enc> --test            restore into a scratch database, show row counts, drop it (touches nothing else)
#   restore.sh <backup.tar.enc> --apply --yes     replace the live database (the current one is KEPT, renamed keygate_before_restore_<ts>)
# Add --with-env to also leave the saved environment next to the live one as keygate.env.restored (0600) for manual review.
set -euo pipefail
umask 077
PASS_FILE=${PASS_FILE:-/etc/keygate/backup.pass}
DB_NAME=${DB_NAME:-keygate}
DB_ROLE=${DB_ROLE:-keygate}
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
file=${1:-}; mode=${2:-}
[ -f "$file" ] || { echo "usage: $0 <backup.tar.enc> --test | --apply --yes [--with-env]" >&2; exit 2; }
shift 2 || true
yes=no; withenv=no
for a in "$@"; do [ "$a" = --yes ] && yes=yes; [ "$a" = --with-env ] && withenv=yes; done

work=$(mktemp -d /var/tmp/keygate-restore.XXXXXX)
trap 'rm -rf "$work"' EXIT
openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 -pass "file:$PASS_FILE" -in "$file" | tar -C "$work" -xf -
[ -s "$work/db.dump" ] || { echo "no db.dump in the archive (wrong passphrase file?)" >&2; exit 1; }
chmod 644 "$work/db.dump"; chmod 755 "$work"   # readable by the postgres user for pg_restore; removed on exit

psql_pg() { runuser -u postgres -- psql -v ON_ERROR_STOP=1 -X -q -At "$@"; }
counts() {
  psql_pg -d "$1" -c "SELECT 'products='||count(*) FROM products UNION ALL SELECT 'plans='||count(*) FROM plans UNION ALL SELECT 'licenses='||count(*) FROM licenses UNION ALL SELECT 'activations='||count(*) FROM activations;"
}

case "$mode" in
  --test)
    scratch=keygate_restore_test
    psql_pg -c "DROP DATABASE IF EXISTS $scratch;" -c "CREATE DATABASE $scratch;"
    runuser -u postgres -- pg_restore --no-owner --exit-on-error -d "$scratch" "$work/db.dump"
    echo "restored into scratch database $scratch:"; counts "$scratch"
    psql_pg -c "DROP DATABASE $scratch;"
    echo "scratch database dropped; the live database was not touched. RESTORE TEST OK"
    ;;
  --apply)
    [ "$yes" = yes ] || { echo "refusing without --yes" >&2; exit 1; }
    ts=$(date -u +%Y%m%dT%H%M%SZ); old="keygate_before_restore_$ts"
    systemctl stop keygate || true
    psql_pg -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='$DB_NAME' AND pid<>pg_backend_pid();" >/dev/null
    psql_pg -c "ALTER DATABASE $DB_NAME RENAME TO $old;"
    psql_pg -c "CREATE DATABASE $DB_NAME OWNER $DB_ROLE;"
    runuser -u postgres -- pg_restore --no-owner --role="$DB_ROLE" --exit-on-error -d "$DB_NAME" "$work/db.dump"
    runuser -u postgres -- psql -X -q -d "$DB_NAME" -c "GRANT ALL ON ALL TABLES IN SCHEMA public TO $DB_ROLE; GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO $DB_ROLE; ALTER SCHEMA public OWNER TO $DB_ROLE;" >/dev/null
    [ "$withenv" = yes ] && { cp "$work/keygate.env" /etc/keygate/keygate.env.restored; chmod 600 /etc/keygate/keygate.env.restored; echo "saved environment copied to /etc/keygate/keygate.env.restored (review, do not overwrite blindly)"; }
    echo "live database replaced; previous one kept as $old (drop it by hand once you are sure)"; counts "$DB_NAME"
    systemctl start keygate
    ;;
  *) echo "usage: $0 <backup.tar.enc> --test | --apply --yes [--with-env]" >&2; exit 2 ;;
esac
