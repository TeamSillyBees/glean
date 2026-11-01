#!/usr/bin/env python3
"""Local safety checks; never emit connection strings or secret contents."""
import json
import os
from pathlib import Path
import stat
import sys
import tarfile
from urllib.parse import parse_qs, unquote, urlsplit


def private_file(path):
    mode = Path(path).stat().st_mode
    if not stat.S_ISREG(mode) or mode & 0o077:
        raise ValueError("Secret environment file must be a regular owner-only file (0600).")


def database(name):
    try:
        url = urlsplit(os.environ[name])
        params = parse_qs(url.query, keep_blank_values=True)
        if url.scheme not in ("postgres", "postgresql") or not url.hostname or not url.path.strip("/") or not url.username:
            raise ValueError
        if (url.fragment or '%' in url.hostname or not url.password or
                not (1 <= (5432 if url.port is None else url.port) <= 65535) or
                any(len(values) != 1 for values in params.values())):
            raise ValueError
        if any('\x00' in unquote(value or '') for value in (url.username, url.password, url.path)):
            raise ValueError
        # Multiple sslmode values are ambiguous across clients; reject them.
        secure = params.get("sslmode") == ["verify-full"]
        test = (os.environ.get("GLEAN_ALLOW_LOCAL_TEST_DB") == "1" and
                url.hostname in ("127.0.0.1", "localhost", "::1") and
                params.get("sslmode") == ["disable"])
        if not secure and not test:
            raise ValueError
        if set(params) - {"sslmode", "sslrootcert", "connect_timeout", "application_name"}:
            raise ValueError
    except (KeyError, ValueError):
        raise ValueError("Database URL must explicitly use verify-full (only explicit loopback test mode permits disable).") from None


def validate_archive(archive):
    with tarfile.open(archive, "r:gz") as source:
        for member in source.getmembers():
            path = Path(member.name)
            if path.is_absolute() or ".." in path.parts or not (member.isfile() or member.isdir()):
                raise ValueError("Data archive contains an unsafe path or special/link entry.")


def extract(archive, target):
    target = Path(target)
    if target.exists():
        raise ValueError("Restored directory must not already exist.")
    validate_archive(archive)
    with tarfile.open(archive, "r:gz") as source:
        target.mkdir(mode=0o700)
        # Validated regular files/directories only; do not restore archived ownership.
        for member in source.getmembers():
            output = target / member.name
            if member.isdir():
                output.mkdir(parents=True, exist_ok=True)
            else:
                output.parent.mkdir(parents=True, exist_ok=True)
                with source.extractfile(member) as src, output.open("xb") as dst:
                    import shutil
                    shutil.copyfileobj(src, dst)
                output.chmod(member.mode & 0o700)


def libpq(name, tool, *args):
    """Run a PostgreSQL client with parsed libpq defaults, never a URI in argv."""
    import subprocess
    if tool not in ("psql", "pg_dump", "pg_restore"):
        raise ValueError("Unsupported PostgreSQL client.")
    database(name)
    url = urlsplit(os.environ[name])
    params = parse_qs(url.query, keep_blank_values=True)
    # PGDATABASE is a database NAME, not a connection URI. Remove inherited
    # libpq defaults/services/options that could redirect this canonical target.
    environment = {key: value for key, value in os.environ.items() if not key.startswith("PG")}
    environment.update({"PGHOST": url.hostname, "PGPORT": str(5432 if url.port is None else url.port),
                        "PGDATABASE": unquote(url.path[1:]),
                        "PGUSER": unquote(url.username), "PGPASSWORD": unquote(url.password),
                        "PGSSLMODE": params["sslmode"][0], "PGPASSFILE": "/dev/null"})
    for query, setting in (("sslrootcert", "PGSSLROOTCERT"),
                           ("connect_timeout", "PGCONNECT_TIMEOUT"),
                           ("application_name", "PGAPPNAME")):
        if query in params:
            environment[setting] = params[query][0]
    result = subprocess.run([tool, "--no-password", *args], env=environment)
    sys.exit(result.returncode)


def compose(root, *args):
    import subprocess
    database("DATABASE_URL")
    url = urlsplit(os.environ["DATABASE_URL"])
    environment = os.environ.copy()
    # These explicit service.environment values override env_file and YAML settings.
    # The connection used by dump/migration is also the backend's only DB identity.
    environment.update({"GLEAN_DATABASE_HOST": url.hostname,
                        "GLEAN_DATABASE_PORT": str(5432 if url.port is None else url.port),
                        "GLEAN_DATABASE_NAME": unquote(url.path[1:]),
                        "GLEAN_DATABASE_USER": unquote(url.username),
                        "GLEAN_DATABASE_PASSWORD": unquote(url.password or "")})
    result = subprocess.run(["docker", "compose", "--env-file", "/dev/null",
                             "--project-name", "glean-production", "-f",
                             str(Path(root)/"deploy/compose.production.yml"), *args], env=environment)
    sys.exit(result.returncode)


def ops_paths():
    paths = [Path(os.environ[key]).resolve() for key in
             ("GLEAN_DATA_DIR", "GLEAN_ORIGINAL_ARCHIVE_DIR", "GLEAN_BACKUP_ROOT", "GLEAN_STATE_DIR")]
    for index, path in enumerate(paths):
        for other in paths[index+1:]:
            if path == other or path in other.parents or other in path.parents:
                raise ValueError("Data, original archives, backup and state paths must be separate, non-nested directories.")


def distinct_restore_database():
    database("DATABASE_URL")
    database("GLEAN_RESTORE_DATABASE_URL")
    def identity(name):
        url = urlsplit(os.environ[name])
        return (url.hostname.lower().rstrip('.'), 5432 if url.port is None else url.port, unquote(url.path[1:]))
    if identity("DATABASE_URL") == identity("GLEAN_RESTORE_DATABASE_URL"):
        raise ValueError("Restore database must differ from the configured source host/port/database, not merely credentials or options.")


def restore_target(path):
    target = Path(path).resolve()
    for key in ("GLEAN_DATA_DIR", "GLEAN_ORIGINAL_ARCHIVE_DIR", "GLEAN_BACKUP_ROOT", "GLEAN_STATE_DIR"):
        protected = Path(os.environ[key]).resolve()
        if target == protected or protected in target.parents or target in protected.parents:
            raise ValueError("Restore directory must be outside every live data/archive/backup/state directory.")


def deployment_identity():
    url = urlsplit(os.environ["DATABASE_URL"])
    return {"database": {"host": url.hostname, "port": 5432 if url.port is None else url.port,
                         "user": unquote(url.username), "name": unquote(url.path[1:])},
            "data": str(Path(os.environ["GLEAN_DATA_DIR"]).resolve()),
            "originals": str(Path(os.environ["GLEAN_ORIGINAL_ARCHIVE_DIR"]).resolve())}


def check_current(path):
    if Path(path).exists():
        current = json.loads(Path(path).read_text(encoding="utf-8"))
        if current.get("identity") != deployment_identity():
            raise ValueError("Database identity or data paths changed: use a separate reviewed recovery/cutover, not routine deployment.")


def record_release(path):
    record = {key: os.environ[key] for key in ("RELEASE_SHA", "ROOT", "GLEAN_BACKEND_IMAGE", "GLEAN_WEB_IMAGE")}
    record["identity"] = deployment_identity()
    temporary = Path(path + ".new")
    temporary.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


if __name__ == "__main__":
    try:
        {"libpq": libpq, "private-file": private_file, "database": database, "extract": extract,
         "distinct-restore-database": distinct_restore_database, "ops-paths": ops_paths, "restore-target": restore_target, "check-current": check_current, "compose": compose, "validate-archive": validate_archive, "record-release": record_release}[sys.argv[1]](*sys.argv[2:])
    except (ValueError, OSError, KeyError, tarfile.TarError) as error:
        # Library errors can contain filenames, but connection contents never enter them.
        print(str(error), file=sys.stderr)
        sys.exit(1)
