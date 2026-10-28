"""Offline contract tests: no production connection, daemon, or credentials required."""
import importlib.util
import io
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('guard', ROOT / 'scripts/production/guard.py')
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class GuardTests(unittest.TestCase):
    def test_database_rejects_insecure_remote_and_identity_overrides(self):
        for url in ('postgres://u:p@remote/db?sslmode=disable',
                    'postgres://u:p@remote/db?sslmode=verify-full&host=other',
                    'postgres://u:p@remote/db?sslmode=verify-full&host=',
                    'postgres://u:p@remote:malformed-secret-port/db?sslmode=verify-full',
                    'postgres://u:p@%72emote/db?sslmode=verify-full',
                    'postgres://u:p@remote/db?sslmode=verify-full#fragment',
                    'postgres://u:p@remote/db?sslmode=verify-full&options=-csearch_path%3Dother',
                    'postgres://u:p@remote/db?sslmode=verify-full&user=other',
                    'postgres://u:p@remote/db?sslmode=verify-full&sslmode=disable'):
            with self.subTest(url=url), patch.dict(os.environ, {'CHECK_URL': url, 'GLEAN_ALLOW_LOCAL_TEST_DB': '1'}):
                with self.assertRaises(ValueError):
                    guard.database('CHECK_URL')
        with patch.dict(os.environ, {'CHECK_URL': 'postgres://u:p@127.0.0.1/db?sslmode=disable', 'GLEAN_ALLOW_LOCAL_TEST_DB': '1'}):
            guard.database('CHECK_URL')

    def test_archive_rejects_traversal_absolute_paths_links_and_special_files(self):
        for name, kind in (('../escape', tarfile.REGTYPE), ('/absolute', tarfile.REGTYPE),
                           ('link', tarfile.SYMTYPE), ('hard', tarfile.LNKTYPE), ('device', tarfile.CHRTYPE)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                archive = Path(directory)/'bad.tar.gz'
                with tarfile.open(archive, 'w:gz') as output:
                    entry = tarfile.TarInfo(name)
                    entry.type = kind
                    entry.linkname = '../escape'
                    output.addfile(entry, io.BytesIO())
                with self.assertRaises(ValueError):
                    guard.extract(archive, Path(directory)/'restored')
                self.assertFalse((Path(directory)/'restored').exists())

    def test_secret_environment_requires_private_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'ops.env'
            path.write_text('placeholder')
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                guard.private_file(path)
            path.chmod(0o600)
            guard.private_file(path)

    def test_restore_database_cannot_alias_source_by_credentials_or_query(self):
        values = {'DATABASE_URL': 'postgres://u:p@db.example/glean?sslmode=verify-full',
                  'GLEAN_RESTORE_DATABASE_URL': 'postgres://different:other@DB.EXAMPLE.:5432/glean?sslmode=verify-full&application_name=restore'}
        with patch.dict(os.environ, values):
            with self.assertRaises(ValueError):
                guard.distinct_restore_database()
            with patch.dict(os.environ, {'GLEAN_RESTORE_DATABASE_URL': 'postgres://u:p@db.example/glean_new?sslmode=verify-full'}):
                guard.distinct_restore_database()

    def test_restore_target_and_backup_paths_cannot_overlap_live_data(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = {key: str(Path(directory)/name) for key, name in
                     (('GLEAN_DATA_DIR', 'data'), ('GLEAN_ORIGINAL_ARCHIVE_DIR', 'originals'),
                      ('GLEAN_BACKUP_ROOT', 'backups'), ('GLEAN_STATE_DIR', 'state'))}
            with patch.dict(os.environ, paths):
                guard.ops_paths()
                guard.restore_target(str(Path(directory)/'isolated'))
                with self.assertRaises(ValueError):
                    guard.restore_target(str(Path(directory)/'data/restored'))
                with patch.dict(os.environ, {'GLEAN_BACKUP_ROOT': str(Path(directory)/'data/backups')}):
                    with self.assertRaises(ValueError):
                        guard.ops_paths()

    def test_compose_derives_one_canonical_database_identity(self):
        values = {'DATABASE_URL': 'postgres://encoded%40user:p%24ss@db.example:5444/glean%20db?sslmode=verify-full'}
        with patch.dict(os.environ, values), patch('subprocess.run') as run:
            run.return_value.returncode = 0
            with self.assertRaises(SystemExit):
                guard.compose('/release', 'config', '--quiet')
            env = run.call_args.kwargs['env']
            self.assertEqual(env['GLEAN_DATABASE_HOST'], 'db.example')
            self.assertEqual(env['GLEAN_DATABASE_PORT'], '5444')
            self.assertEqual(env['GLEAN_DATABASE_USER'], 'encoded@user')
            self.assertEqual(env['GLEAN_DATABASE_PASSWORD'], 'p$ss')
            self.assertEqual(env['GLEAN_DATABASE_NAME'], 'glean db')


class DeploymentTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.work = Path(self.directory.name)
        self.release = self.work/'release'
        shutil.copytree(ROOT/'scripts/production', self.release/'scripts/production')
        shutil.copytree(ROOT/'deploy', self.release/'deploy')
        self.sha = 'a'*40
        (self.release/'deploy/REVISION').write_text(self.sha+'\n')
        for name in ('data', 'originals', 'configs', 'bin'):
            (self.work/name).mkdir()
        (self.work/'data/upload').write_text('upload-sentinel')
        (self.work/'originals/private.md').write_text('private-original-sentinel')
        (self.work/'backend.env').write_text('INITRA_IDGEN_NODE=1\n')
        (self.work/'backend.env').chmod(0o600)
        values = {'DATABASE_URL': 'postgres://operator:never-log-this-secret@db.example/glean?sslmode=verify-full',
                  'GLEAN_DATA_DIR': str(self.work/'data'), 'GLEAN_ORIGINAL_ARCHIVE_DIR': str(self.work/'originals'),
                  'GLEAN_BACKUP_ROOT': str(self.work/'backups'), 'GLEAN_STATE_DIR': str(self.work/'state'),
                  'GLEAN_BACKEND_ENV': str(self.work/'backend.env'), 'GLEAN_CONFIG_DIR': str(self.work/'configs')}
        ops = self.work/'ops.env'
        ops.write_text(''.join(k+'='+shlex.quote(v)+'\n' for k,v in values.items()))
        ops.chmod(0o600)
        self.env = {**os.environ, 'PATH': str(self.work/'bin')+':'+os.environ['PATH'],
                    'GLEAN_OPS_ENV': str(ops), 'TEST_LOG': str(self.work/'commands.log'),
                    'GLEAN_ALLOW_LOCAL_TEST_DB': '0'}
        for tool in ('docker', 'atlas', 'pg_dump', 'pg_restore', 'curl', 'sleep'):
            path = self.work/'bin'/tool
            path.write_text('''#!/usr/bin/env python3
import os, sys
from pathlib import Path
name = Path(sys.argv[0]).name
with open(os.environ['TEST_LOG'], 'a') as log: log.write(name+' '+' '.join(sys.argv[1:])+'\\n')
if name == 'pg_dump':
    target = next(a.split('=',1)[1] for a in sys.argv if a.startswith('--file='))
    Path(target).write_text('synthetic dump for shell sequencing test only')
if name == os.environ.get('TEST_FAIL'): sys.exit(1)
''')
            path.chmod(0o755)
        self.images = ['ghcr.io/example/backend@sha256:'+'b'*64, 'ghcr.io/example/web@sha256:'+'c'*64]

    def run_deploy(self, fail=''):
        self.env['TEST_FAIL'] = fail
        return subprocess.run(['bash', str(self.release/'scripts/production/deploy.sh'), self.sha, *self.images],
                              env=self.env, capture_output=True, text=True)

    def test_deployment_orders_stop_backup_migrate_start_readiness(self):
        result = self.run_deploy()
        self.assertEqual(result.returncode, 0, result.stderr)
        log = (self.work/'commands.log').read_text()
        self.assertLess(log.index('stop backend'), log.index('pg_dump'))
        self.assertLess(log.index('pg_dump'), log.index('atlas'))
        self.assertLess(log.index('atlas'), log.index('up -d'))
        self.assertIn('8080/ready', log)
        self.assertTrue((self.work/'state/current.json').exists())
        self.assertNotIn('never-log-this-secret', result.stdout+result.stderr+log)

    def test_backup_or_migration_failure_never_starts_backend(self):
        for tool in ('pg_dump', 'atlas'):
            with self.subTest(tool=tool):
                (self.work/'commands.log').write_text('')
                result = self.run_deploy(tool)
                self.assertNotEqual(result.returncode, 0)
                log = (self.work/'commands.log').read_text()
                self.assertNotIn('up -d', log)
                self.assertTrue(log.rstrip().endswith('stop backend'))
                self.assertFalse((self.work/'state/current.json').exists())

    def test_failed_readiness_stops_backend_without_database_restore(self):
        result = self.run_deploy('curl')
        self.assertNotEqual(result.returncode, 0)
        log = (self.work/'commands.log').read_text()
        self.assertTrue(log.rstrip().endswith('stop backend'))
        self.assertNotIn('--clean', log)
        self.assertNotIn('--dbname', log)
        self.assertFalse((self.work/'state/current.json').exists())

    def test_revision_mismatch_fails_before_any_server_command(self):
        (self.release/'deploy/REVISION').write_text('d'*40)
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())


if __name__ == '__main__':
    unittest.main()
