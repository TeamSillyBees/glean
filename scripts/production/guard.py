#!/usr/bin/env python3
"""Local safety checks; never emit connection strings or secret contents."""
import json
import os
import re
from pathlib import Path
import stat
import sys
import tarfile
from urllib.parse import parse_qs, unquote, urlsplit


# The same deliberately restricted grammar is used before SSH interpolation.
# No shell metacharacters, Compose interpolation, empty/dot components or aliases.
LAYOUT = {
    "GLEAN_OPS_ENV": "config/ops.env", "GLEAN_BACKEND_ENV": "config/backend.env",
    "GLEAN_CONFIG_DIR": "config/configs", "GLEAN_DATA_DIR": "data",
    "GLEAN_ORIGINAL_ARCHIVE_DIR": "originals", "GLEAN_BACKUP_ROOT": "backups",
    "GLEAN_STATE_DIR": "state",
}


def safe_path(value):
    if (not isinstance(value, str) or not re.fullmatch(r"/(?:[A-Za-z0-9._-]+/)*[A-Za-z0-9._-]+", value)
            or any(part in (".", "..") for part in value.split("/"))):
        raise ValueError("Paths must be canonical absolute non-root paths with ASCII letters, digits, dots, underscores and hyphens only.")
    path = Path(value)
    for component in (*reversed(path.parents), path):
        if component.is_symlink():
            raise ValueError("Operational paths must not contain symlinks.")
    return path


def private_directory(path):
    if path.exists():
        info = path.stat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise ValueError("Operational control directories must be owned by the current operator with mode 0700.")


def base_directory():
    base = safe_path(os.environ.get("PRODUCTION_BASE_DIR", ""))
    if not base.is_dir():
        raise ValueError("PRODUCTION_BASE_DIR must be provisioned before running operations.")
    private_directory(base)
    return base


def layout():
    base = base_directory()
    for key, suffix in LAYOUT.items():
        expected = str(base / suffix)
        if os.environ.get(key, expected) != expected:
            raise ValueError("Operational path overrides must match PRODUCTION_BASE_DIR's fixed layout; use a reviewed migration.")
        safe_path(expected)
    for suffix in ("config", "releases", "backups", "state", "recovery"):
        path = safe_path(str(base / suffix))
        private_directory(path)
    if (base / "state/current.json.new").exists():
        raise ValueError("Unfinished release state exists; inspect current.json.new before retrying.")
    # Files opened by shell redirection/cp must never redirect writes elsewhere.
    for name in ("operations.lock", "current.json", "previous.json", "current.json.new"):
        path = safe_path(str(base / "state" / name))
        if path.exists() and (not path.is_file() or path.stat().st_nlink != 1):
            raise ValueError("State entries must be regular, non-linked files.")


def release_path(root):
    base = base_directory()
    path = safe_path(root)
    if path.parent != base / "releases" or not path.is_dir():
        raise ValueError("Deployment must run from one retained release directly below PRODUCTION_BASE_DIR/releases.")


def active_mounts():
    """Prevent a fresh base/state directory from silently switching an existing project."""
    import subprocess
    result = subprocess.run(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=glean-production",
                             "--filter", "label=com.docker.compose.service=backend"],
                            capture_output=True, text=True)
    if result.returncode:
        raise ValueError("Unable to inspect existing production containers.")
    ids = result.stdout.split()
    if not ids:
        return
    if any(not re.fullmatch(r"[0-9a-f]{12,64}", value) for value in ids):
        raise ValueError("Unable to validate existing production containers.")
    result = subprocess.run(["docker", "inspect", *ids], capture_output=True, text=True)
    if result.returncode:
        raise ValueError("Unable to inspect existing production mounts.")
    records = json.loads(result.stdout)
    if not isinstance(records, list) or len(records) != len(ids):
        raise ValueError("Unable to validate existing production mounts.")
    expected = {"/app/var": os.environ["GLEAN_DATA_DIR"], "/app/configs": os.environ["GLEAN_CONFIG_DIR"]}
    for record in records:
        if not isinstance(record, dict) or not isinstance(record.get("Mounts"), list):
            raise ValueError("Unable to validate existing production mounts.")
        if any(not isinstance(entry, dict) or "Destination" not in entry for entry in record["Mounts"]):
            raise ValueError("Unable to validate existing production mounts.")
        mounts = {entry["Destination"]: entry for entry in record["Mounts"]}
        for target, source in expected.items():
            mount = mounts.get(target, {})
            if mount.get("Type") != "bind" or mount.get("Source") != source:
                raise ValueError("Existing production mounts differ: a reviewed manual migration is required before changing base/data/config paths.")


def private_file(path):
    path = safe_path(str(path))
    info = path.stat()
    mode = info.st_mode
    if not stat.S_ISREG(mode) or mode & 0o077 or info.st_uid != os.geteuid() or info.st_nlink != 1:
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
    layout()
    # Fixed sibling layout prevents recursive backup inclusion or path overlaps.
    for key, suffix in LAYOUT.items():
        if os.environ.get(key) != str(base_directory() / suffix):
            raise ValueError("All operational paths must be derived from PRODUCTION_BASE_DIR.")


def distinct_restore_database():
    database("DATABASE_URL")
    database("GLEAN_RESTORE_DATABASE_URL")
    def identity(name):
        url = urlsplit(os.environ[name])
        return (url.hostname.lower().rstrip('.'), 5432 if url.port is None else url.port, unquote(url.path[1:]))
    if identity("DATABASE_URL") == identity("GLEAN_RESTORE_DATABASE_URL"):
        raise ValueError("Restore database must differ from the configured source host/port/database, not merely credentials or options.")


def restore_target(path):
    target = safe_path(path)
    base = base_directory()
    if target.parent != base / "recovery" or target.exists():
        raise ValueError("Restore target must be a new directory directly below PRODUCTION_BASE_DIR/recovery.")
    private_directory(base / "recovery")


def backup_path(path):
    destination = safe_path(path)
    if destination.parent != base_directory() / "backups":
        raise ValueError("Backup must be directly below PRODUCTION_BASE_DIR/backups.")
    if destination.exists():
        raise ValueError("Backup destination already exists.")


def existing_backup(path):
    directory = safe_path(path)
    if directory.parent != base_directory() / "backups" or not directory.is_dir():
        raise ValueError("Backup input must be a directory directly below PRODUCTION_BASE_DIR/backups.")
    private_directory(directory)
    for name in ("COMPLETE", "SHA256SUMS", "database.dump", "data.tar.gz", "originals.tar.gz", "REVISION"):
        private_file(directory / name)


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
    # Exclusive creation also refuses stale files from an interrupted write.
    with temporary.open("x", encoding="utf-8") as output:
        output.write(json.dumps(record, indent=2) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


if __name__ == "__main__":
    try:
        {"existing-backup": existing_backup, "layout": layout, "release-path": release_path, "active-mounts": active_mounts, "backup-path": backup_path, "libpq": libpq, "private-file": private_file, "database": database, "extract": extract,
         "distinct-restore-database": distinct_restore_database, "ops-paths": ops_paths, "restore-target": restore_target, "check-current": check_current, "compose": compose, "validate-archive": validate_archive, "record-release": record_release}[sys.argv[1]](*sys.argv[2:])
    except (ValueError, OSError, KeyError, tarfile.TarError) as error:
        # Library errors can contain filenames, but connection contents never enter them.
        print(str(error), file=sys.stderr)
        sys.exit(1)
