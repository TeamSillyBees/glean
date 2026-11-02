"""Offline contract tests: no production connection, daemon, or credentials required."""
import importlib.util
import io
import json
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

    def test_database_ports_reject_invalid_values_before_client_actions(self):
        for authority in ('db.example:0', 'db.example:65536', 'db.example:not-a-port',
                          'db.example:5432:5433', '[::1'):
            url = f'postgres://u:never-log-this-secret@{authority}/glean?sslmode=verify-full'
            with self.subTest(authority=authority), patch.dict(os.environ, {'DATABASE_URL': url}), patch('subprocess.run') as run:
                for action in (lambda: guard.database('DATABASE_URL'),
                               lambda: guard.libpq('DATABASE_URL', 'pg_dump'),
                               lambda: guard.compose('/release', 'config', '--quiet')):
                    with self.assertRaises(ValueError) as result:
                        action()
                    self.assertNotIn('never-log-this-secret', str(result.exception))
                run.assert_not_called()

    def test_database_ports_preserve_defaults_and_valid_explicit_values(self):
        for suffix, port in (('', 5432), (':1', 1), (':5432', 5432), (':5444', 5444), (':65535', 65535)):
            values = {'DATABASE_URL': f'postgres://u:p@db.example{suffix}/glean?sslmode=verify-full',
                      'GLEAN_DATA_DIR': '/test/data', 'GLEAN_ORIGINAL_ARCHIVE_DIR': '/test/originals'}
            with self.subTest(suffix=suffix), patch.dict(os.environ, values), patch('subprocess.run') as run:
                guard.database('DATABASE_URL')
                self.assertEqual(guard.deployment_identity()['database']['port'], port)
                run.return_value.returncode = 0
                for action, setting in ((lambda: guard.libpq('DATABASE_URL', 'pg_dump'), 'PGPORT'),
                                        (lambda: guard.compose('/release', 'config', '--quiet'), 'GLEAN_DATABASE_PORT')):
                    with self.assertRaises(SystemExit) as result:
                        action()
                    self.assertEqual(result.exception.code, 0)
                    self.assertEqual(run.call_args.kwargs['env'][setting], str(port))

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

    def test_base_rejects_ambiguous_and_shell_unsafe_paths(self):
        for value in ('', '/', 'relative', '/base/', '//base', '/a//b', '/a/../b', '/a/./b',
                      '/a b', '/a\nb', "/a'b", '/a;touch-x', '/a$(id)', '/a${HOME}', '/a:other', '/中文'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                guard.safe_path(value)
        self.assertEqual(str(guard.safe_path('/srv/glean-v1.0_test')), '/srv/glean-v1.0_test')

    def test_restore_target_and_backup_paths_cannot_overlap_live_data(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = {key: str(Path(directory)/name) for key, name in guard.LAYOUT.items()}
            paths['PRODUCTION_BASE_DIR'] = directory
            with patch.dict(os.environ, paths):
                guard.ops_paths()
                guard.restore_target(str(Path(directory)/'recovery/isolated'))
                guard.backup_path(str(Path(directory)/'backups/new'))
                for target in ('data/restored', 'backups/restored', 'state', 'recovery', '../outside'):
                    with self.subTest(target=target), self.assertRaises(ValueError):
                        guard.restore_target(directory+'/'+target)
                with patch.dict(os.environ, {'GLEAN_BACKUP_ROOT': str(Path(directory)/'data/backups')}):
                    with self.assertRaises(ValueError):
                        guard.ops_paths()

    def test_layout_rejects_symlinks_in_base_children_and_state_files(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryDirectory() as outside:
            base = Path(directory)
            with patch.dict(os.environ, {'PRODUCTION_BASE_DIR': directory}, clear=True):
                for suffix in ('data', 'config', 'backups', 'state', 'recovery', 'releases'):
                    link = base/suffix
                    link.symlink_to(outside)
                    with self.subTest(suffix=suffix), self.assertRaises(ValueError):
                        guard.layout()
                    link.unlink()
                (base/'state').mkdir(mode=0o700)
                for name in ('operations.lock', 'current.json', 'previous.json', 'current.json.new'):
                    link = base/'state'/name
                    link.symlink_to(Path(outside)/'absent')
                    with self.subTest(name=name), self.assertRaises(ValueError):
                        guard.layout()
                    link.unlink()
                alias = base/'alias'
                alias.symlink_to(outside)
                with patch.dict(os.environ, {'PRODUCTION_BASE_DIR': str(alias)}), self.assertRaises(ValueError):
                    guard.layout()

    def test_existing_container_mounts_block_base_switch_even_without_state(self):
        values = {'GLEAN_DATA_DIR': '/new/data', 'GLEAN_CONFIG_DIR': '/new/config/configs'}
        with patch.dict(os.environ, values), patch('subprocess.run') as run:
            run.return_value.returncode = 0
            run.return_value.stdout = 'a'*12
            for source in ('/old/data', '/new/data'):
                records = [{'Mounts': [{'Destination': '/app/var', 'Source': source, 'Type': 'bind'},
                                       {'Destination': '/app/configs', 'Source': '/new/config/configs', 'Type': 'bind'}]}]
                run.side_effect = [subprocess.CompletedProcess([], 0, 'a'*12),
                                   subprocess.CompletedProcess([], 0, json.dumps(records))]
                if source.startswith('/old'):
                    with self.assertRaises(ValueError):
                        guard.active_mounts()
                else:
                    guard.active_mounts()
            run.side_effect = [subprocess.CompletedProcess([], 1, '', 'private daemon error')]
            with self.assertRaisesRegex(ValueError, 'Unable to inspect'):
                guard.active_mounts()

    def test_libpq_clients_receive_discrete_environment_never_uri_or_inherited_target(self):
        values = {'CHECK_URL': 'postgres://encoded%40user:p%24ss@db.example:5444/glean%20db?sslmode=verify-full&sslrootcert=%2Fprivate%2Fca.crt&connect_timeout=5&application_name=backup',
                  'PGDATABASE': 'postgres://wrong:secret@wrong/other', 'PGHOSTADDR': '192.0.2.1',
                  'PGSERVICE': 'wrong-service', 'PGOPTIONS': '-csearch_path=wrong',
                  'PGSSLCERT': '/unrelated/client.crt'}
        for tool in ('psql', 'pg_dump', 'pg_restore'):
            with self.subTest(tool=tool), patch.dict(os.environ, values), patch('subprocess.run') as run:
                run.return_value.returncode = 0
                with self.assertRaises(SystemExit) as result:
                    guard.libpq('CHECK_URL', tool, '--version')
                self.assertEqual(result.exception.code, 0)
                self.assertEqual(run.call_args.args[0], [tool, '--no-password', '--version'])
                env = run.call_args.kwargs['env']
                self.assertEqual(env['PGHOST'], 'db.example')
                self.assertEqual(env['PGPORT'], '5444')
                self.assertEqual(env['PGDATABASE'], 'glean db')
                self.assertEqual(env['PGUSER'], 'encoded@user')
                self.assertEqual(env['PGPASSWORD'], 'p$ss')
                self.assertEqual(env['PGSSLMODE'], 'verify-full')
                self.assertEqual(env['PGSSLROOTCERT'], '/private/ca.crt')
                self.assertEqual(env['PGCONNECT_TIMEOUT'], '5')
                self.assertEqual(env['PGAPPNAME'], 'backup')
                self.assertEqual(env['PGPASSFILE'], '/dev/null')
                for key in ('PGHOSTADDR', 'PGSERVICE', 'PGOPTIONS', 'PGSSLCERT'):
                    self.assertNotIn(key, env)

    def test_shell_database_calls_all_use_the_validated_libpq_wrapper(self):
        for name in ('common.sh', 'restore.sh', 'tests/recovery-drill.sh'):
            content = (ROOT/'scripts/production'/name).read_text()
            self.assertNotIn('PGDATABASE=', content)
            for line in content.splitlines():
                if 'psql -X' in line or 'pg_dump --format' in line or 'pg_restore --' in line:
                    self.assertIn('pg_client ', line)

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
        self.release = self.work/'releases/release'
        (self.work/'releases').mkdir(mode=0o700)
        shutil.copytree(ROOT/'scripts/production', self.release/'scripts/production')
        shutil.copytree(ROOT/'deploy', self.release/'deploy')
        self.sha = 'a'*40
        (self.release/'deploy/REVISION').write_text(self.sha+'\n')
        for name in ('data', 'originals', 'config', 'config/configs', 'bin'):
            (self.work/name).mkdir(mode=0o700)
        (self.work/'data/upload').write_text('upload-sentinel')
        (self.work/'originals/private.md').write_text('private-original-sentinel')
        (self.work/'config/backend.env').write_text('INITRA_IDGEN_NODE=1\n')
        (self.work/'config/backend.env').chmod(0o600)
        values = {'DATABASE_URL': 'postgres://operator:never-log-this-secret@db.example/glean?sslmode=verify-full'}
        ops = self.work/'config/ops.env'
        ops.write_text(''.join(k+'='+shlex.quote(v)+'\n' for k,v in values.items()))
        ops.chmod(0o600)
        self.env = {**os.environ, 'PATH': str(self.work/'bin')+':'+os.environ['PATH'],
                    'PRODUCTION_BASE_DIR': str(self.work), 'TEST_LOG': str(self.work/'commands.log'),
                    'GLEAN_ALLOW_LOCAL_TEST_DB': '0'}
        for tool in ('docker', 'atlas', 'psql', 'pg_dump', 'pg_restore', 'curl', 'sleep'):
            path = self.work/'bin'/tool
            path.write_text('''#!/usr/bin/env python3
import os, sys
from pathlib import Path
name = Path(sys.argv[0]).name
with open(os.environ['TEST_LOG'], 'a') as log: log.write(name+' '+' '.join(sys.argv[1:])+'\\n')
if name == 'psql': print('0')
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

    def test_invalid_database_ports_fail_before_deployment_or_backup_actions(self):
        ops = self.work/'config/ops.env'
        original = ops.read_text()
        for authority in ('db.example:0', 'db.example:65536', 'db.example:not-a-port',
                          'db.example:5432:5433', '[::1'):
            url = f'postgres://operator:never-log-this-secret@{authority}/glean?sslmode=verify-full'
            ops.write_text(original + 'DATABASE_URL=' + shlex.quote(url) + '\n')
            for script, args in (('deploy.sh', [self.sha, *self.images]),
                                 ('backup.sh', ['--writers-stopped'])):
                with self.subTest(authority=authority, script=script):
                    log = self.work/'commands.log'
                    before_log = log.read_bytes() if log.exists() else None
                    before_backups = list((self.work/'backups').iterdir()) if (self.work/'backups').exists() else []
                    result = subprocess.run(['bash', str(self.release/'scripts/production'/script), *args],
                                            env=self.env, capture_output=True, text=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn('never-log-this-secret', result.stdout + result.stderr)
                    self.assertEqual(log.read_bytes() if log.exists() else None, before_log)
                    self.assertEqual(list((self.work/'backups').iterdir()), before_backups)

    def test_base_is_explicit_and_ops_cannot_redirect_paths(self):
        ops = self.work/'config/ops.env'
        original = ops.read_text()
        for addition in ('GLEAN_DATA_DIR=/outside', 'GLEAN_STATE_DIR=/outside',
                         'PRODUCTION_BASE_DIR=/outside', 'GLEAN_BACKUP_ROOT="$PRODUCTION_BASE_DIR/data"'):
            with self.subTest(addition=addition):
                ops.write_text(original+'\n'+addition+'\n')
                result = self.run_deploy()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.work/'commands.log').exists())
        ops.write_text(original)
        del self.env['PRODUCTION_BASE_DIR']
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())

    def test_state_path_symlink_refused_before_any_server_command(self):
        (self.work/'state').mkdir(mode=0o700)
        sentinel = self.work/'sentinel'
        sentinel.write_text('do not modify')
        for name in ('current.json', 'current.json.new', 'previous.json', 'operations.lock'):
            link = self.work/'state'/name
            link.symlink_to(sentinel)
            with self.subTest(name=name):
                result = self.run_deploy()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(sentinel.read_text(), 'do not modify')
                self.assertFalse((self.work/'commands.log').exists())
            link.unlink()

    def test_existing_state_identity_still_blocks_path_change(self):
        (self.work/'state').mkdir(mode=0o700)
        (self.work/'state/current.json').write_text(json.dumps({'identity': {'data': '/legacy/data'}}))
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('paths changed', result.stderr)
        self.assertFalse((self.work/'commands.log').exists())

    def test_ops_symlink_and_external_release_are_refused(self):
        ops = self.work/'config/ops.env'
        moved = self.work/'ops-saved'
        ops.rename(moved)
        ops.symlink_to(moved)
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())
        ops.unlink()
        moved.rename(ops)
        self.release.rename(self.work/'outside-release')
        self.release = self.work/'outside-release'
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())

    def test_backup_restore_files_stay_inside_selected_base(self):
        result = subprocess.run(['bash', str(self.release/'scripts/production/backup.sh'), '--writers-stopped'],
                                env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        backup, = list((self.work/'backups').iterdir())
        self.env['GLEAN_RESTORE_DATABASE_URL'] = 'postgres://operator:p@db.example/recovery?sslmode=verify-full'
        target = self.work/'recovery/restored'
        result = subprocess.run(['bash', str(self.release/'scripts/production/restore.sh'), str(backup), str(target), '--confirm-empty-target'],
                                env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((target/'data/upload').read_text(), 'upload-sentinel')
        self.assertEqual((target/'originals/private.md').read_text(), 'private-original-sentinel')
        for directory in (self.work/'state', self.work/'backups', backup, self.work/'recovery', target):
            self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        log_before = (self.work/'commands.log').read_text()
        for output in (self.work/'data/restored', self.work/'outside', self.work/'backups/nested'):
            result = subprocess.run(['bash', str(self.release/'scripts/production/restore.sh'), str(backup), str(output), '--confirm-empty-target'],
                                    env=self.env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual((self.work/'commands.log').read_text(), log_before)

    def test_stale_state_write_fails_before_mutating_services(self):
        (self.work/'state').mkdir(mode=0o700)
        (self.work/'state/current.json.new').write_text('unfinished')
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())

    def test_revision_mismatch_fails_before_any_server_command(self):
        (self.release/'deploy/REVISION').write_text('d'*40)
        result = self.run_deploy()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work/'commands.log').exists())


if __name__ == '__main__':
    unittest.main()
