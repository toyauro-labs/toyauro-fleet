import json
import os
import subprocess
import tempfile
import unittest
import unicodedata
import uuid
from pathlib import Path

# These tests pin what agents depend on: flags, exit codes and JSON. They run the
# binary, from $FLEET_BIN where there is no toolchain, otherwise a fresh build.
PACKAGE = Path(__file__).resolve().parents[1]
CLI = os.environ.get('FLEET_BIN')
_build_dir = None

def setUpModule():
    global CLI, _build_dir
    if not CLI:
        _build_dir = tempfile.TemporaryDirectory()
        CLI = str(Path(_build_dir.name) / 'fleet')
        subprocess.run(['go', 'build', '-o', CLI, '.'], cwd=PACKAGE, check=True)

def tearDownModule():
    if _build_dir:
        _build_dir.cleanup()

class FleetTest(unittest.TestCase):
    def setUp(self):
        self.server = 'fleet-test-' + uuid.uuid4().hex[:12]
        subprocess.run(['tmux', '-L', self.server, '-f', '/dev/null', 'new-session', '-d', '-s', 'one', 'sleep 120'], check=True)
        # Never read the user's config: it can restore real workstations and agents here.
        subprocess.run(['tmux', '-L', self.server, '-f', '/dev/null', 'new-session', '-d', '-s', 'two', 'sleep 120'], check=True)
        panes = subprocess.check_output(['tmux', '-L', self.server, 'list-panes', '-a', '-F', '#{pane_id}'], text=True).splitlines()
        self.a, self.b = panes

    def tearDown(self):
        subprocess.run(['tmux', '-L', self.server, 'kill-server'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def run_cli(self, *args, ok=True):
        p = subprocess.run([CLI, '--server', self.server, *args], text=True, capture_output=True, timeout=10)
        if ok: self.assertEqual(p.returncode, 0, p.stderr)
        else: self.assertNotEqual(p.returncode, 0)
        return p

    def test_round_trip_across_sessions(self):
        panes = json.loads(self.run_cli('list', '--all', '--json').stdout)['panes']
        self.assertEqual({p['pane'] for p in panes}, {self.a, self.b})
        self.assertEqual(json.loads(self.run_cli('inbox', '--pane', self.b).stdout), [])
        result = json.loads(self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', '你好\nworld').stdout)
        inbox = json.loads(self.run_cli('inbox', '--pane', self.b).stdout)
        self.assertEqual(inbox[0]['body'], '你好\nworld')
        self.assertEqual(inbox[0]['id'], result['id'])
        self.assertEqual(json.loads(self.run_cli('inbox', '--pane', self.a).stdout), [])
        self.run_cli('ack', '--pane', self.a, result['id'], ok=False)
        reply = json.loads(self.run_cli('reply', '--pane', self.b, '--from', f'./{self.server}/{self.b}', '--body', '收到', result['id']).stdout)
        answer = json.loads(self.run_cli('inbox', '--pane', self.a).stdout)[0]
        self.assertEqual(answer['reply_to'], result['id'])
        self.assertEqual(answer['thread'], result['thread'])
        self.assertEqual(answer['id'], reply['id'])
        self.run_cli('ack', '--pane', self.b, result['id'])
        self.assertEqual(json.loads(self.run_cli('inbox', '--pane', self.b).stdout), [])

    def test_across_windows_in_one_session(self):
        subprocess.run(['tmux', '-L', self.server, 'new-window', '-d', '-t', 'one', 'sleep 120'], check=True)
        panes = json.loads(self.run_cli('list', '--all', '--json').stdout)['panes']
        other = next(p['pane'] for p in panes if p['session'] == 'one' and p['pane'] != self.a)
        sent = json.loads(self.run_cli('send', '--to', other, '--from', f'./{self.server}/{self.a}', '--body', 'window test').stdout)
        got = json.loads(self.run_cli('inbox', '--pane', other).stdout)
        self.assertEqual(got[0]['id'], sent['id'])
        self.assertEqual(got[0]['body'], 'window test')

    def test_configured_fleet_lists_all_nodes_and_live_alias(self):
        other = 'fleet-test-' + uuid.uuid4().hex[:12]
        subprocess.run(['tmux', '-L', other, '-f', '/dev/null', 'new-session', '-d', '-s', 'phone', 'sleep 120'], check=True)
        try:
            pane = subprocess.check_output(['tmux', '-L', other, 'display-message', '-p', '-t', 'phone', '#{pane_id}'], text=True).strip()
            subprocess.run(['tmux', '-L', other, 'set-option', '-p', '-t', pane, '@pane_name', 'phone use'], check=True)
            subprocess.run(['tmux', '-L', self.server, 'set-option', '-p', '-t', self.a, '@pane_name', 'mac work'], check=True)
            with tempfile.TemporaryDirectory() as tmp:
                config = Path(tmp) / 'config.json'
                config.write_text(json.dumps({'nodes': [{'name': 'macmini', 'server': self.server}, {'name': 'phone', 'server': other}]}))
                env = dict(os.environ, FLEET_CONFIG=str(config))
                result = subprocess.run([CLI, 'list', '--json'], env=env, text=True, capture_output=True, check=True)
                discovered = json.loads(result.stdout)
                self.assertEqual(discovered['errors'], [])
                self.assertIn(('phone', pane, 'phone use'), [(p['device'], p['pane'], p['name']) for p in discovered['panes']])
                subprocess.run(['tmux', '-L', other, 'set-option', '-p', '-t', pane, '@pane_name', 'renamed'], check=True)
                output = subprocess.check_output([CLI, 'list', '--json'], env=env, text=True)
                self.assertIn('renamed', output)
                subprocess.run(['tmux', '-L', other, 'kill-server'], check=True)
                failed = subprocess.run([CLI, 'list', '--json'], env=env, text=True, capture_output=True)
                self.assertNotEqual(failed.returncode, 0)
                self.assertEqual(json.loads(failed.stdout)['errors'][0]['device'], 'phone')
        finally:
            subprocess.run(['tmux', '-L', other, 'kill-server'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def test_human_output_fits_wide_and_narrow_terminals(self):
        subprocess.run(['tmux', '-L', self.server, 'set-option', '-p', '-t', self.a, '@pane_name', '手机工位'], check=True)
        subprocess.run(['tmux', '-L', self.server, 'set-option', '-p', '-t', self.a, '@agent_adon_state', 'idle'], check=True)
        cmd = [CLI, '--server', self.server, 'list', '--all']
        width = lambda text: sum(0 if unicodedata.combining(c) else 2 if unicodedata.east_asian_width(c) in ('W', 'F') else 1 for c in text)
        wide = subprocess.check_output(cmd, env=dict(os.environ, COLUMNS='140'), text=True)
        rows = [line for line in wide.splitlines() if line.startswith('│')]
        self.assertTrue(any('手机工位' in row for row in rows))
        self.assertEqual(len({width(row) for row in rows}), 1)
        self.assertIn('● idle', wide)
        self.assertNotIn('\x1b[', wide)  # redirected output does not contain terminal colors
        narrow = subprocess.check_output(cmd, env=dict(os.environ, COLUMNS='40'), text=True)
        self.assertIn('手机工位', narrow)
        self.assertIn(self.a, narrow)
        self.assertTrue(all(width(line) <= 40 for line in narrow.splitlines()))

    def test_retry_with_the_same_id_overwrites_rather_than_duplicates(self):
        sent = json.loads(self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', 'first').stdout)
        again = json.loads(self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', 'retry', '--id', sent['id']).stdout)
        self.assertEqual(again['id'], sent['id'])
        inbox = json.loads(self.run_cli('inbox', '--pane', self.b).stdout)
        self.assertEqual([(m['id'], m['body']) for m in inbox], [(sent['id'], 'retry')])
        reply_id = uuid.uuid4().hex
        for _ in range(2):
            self.run_cli('reply', '--pane', self.b, '--from', f'./{self.server}/{self.b}', '--body', 'ok', '--id', reply_id, sent['id'])
        self.assertEqual([m['id'] for m in json.loads(self.run_cli('inbox', '--pane', self.a).stdout)], [reply_id])
        bad = self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', 'x', '--id', 'not-an-id', ok=False)
        self.assertEqual(bad.stderr.strip(), 'fleet: --id must be 32 lowercase hex characters')

    def test_inside_tmux_the_server_defaults_to_the_current_one(self):
        socket = subprocess.check_output(['tmux', '-L', self.server, 'display-message', '-p', '#{socket_path}'], text=True).strip()
        sent = json.loads(self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', 'hi').stdout)
        env = dict(os.environ, TMUX=f'{socket},1,0', TMUX_PANE=self.b)
        inbox = subprocess.run([CLI, 'inbox'], env=env, text=True, capture_output=True, timeout=10)
        self.assertEqual(inbox.returncode, 0, inbox.stderr)
        self.assertEqual([m['id'] for m in json.loads(inbox.stdout)], [sent['id']])
        # With --host the server is the other device's, so it does not follow this one.
        remote = subprocess.run([CLI, '--host', 'nosuchhost.invalid', 'inbox'], env=env, text=True, capture_output=True, timeout=30)
        self.assertNotEqual(remote.returncode, 0)
        self.assertNotIn(self.server, remote.stderr)

    def test_inside_tmux_the_return_address_is_filled_in(self):
        socket = subprocess.check_output(['tmux', '-L', self.server, 'display-message', '-p', '#{socket_path}'], text=True).strip()
        as_pane = lambda pane, **extra: dict(os.environ, TMUX=f'{socket},1,0', TMUX_PANE=pane, **extra)
        run = lambda env, *args: subprocess.run([CLI, *args], env=env, text=True, capture_output=True, timeout=10)
        sent = run(as_pane(self.a), 'send', '--to', self.b, '--body', 'hi')
        self.assertEqual(sent.returncode, 0, sent.stderr)
        request = json.loads(run(as_pane(self.b), 'inbox').stdout)[0]
        self.assertEqual(request['from'], f'./{self.server}/{self.a}')
        replied = run(as_pane(self.b), 'reply', '--body', 'ok', request['id'])
        self.assertEqual(replied.returncode, 0, replied.stderr)
        answer = json.loads(run(as_pane(self.a), 'inbox').stdout)[0]
        self.assertEqual((answer['reply_to'], answer['from']), (request['id'], f'./{self.server}/{self.b}'))
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / 'config.json'
            # Across devices the address is the one the other device uses to reach this one.
            config.write_text(json.dumps({'nodes': [{'name': 'here', 'server': self.server},
                                                    {'name': 'phone', 'host': 'nosuchhost.invalid', 'return_host': 'mac'}]}))
            remote = run(as_pane(self.a, FLEET_CONFIG=str(config)), '--host', 'nosuchhost.invalid', 'send', '--to', '%0', '--body', 'x')
            self.assertNotIn('--from is required', remote.stderr)
            config.write_text(json.dumps({'nodes': [{'name': 'here', 'server': self.server},
                                                    {'name': 'phone', 'host': 'nosuchhost.invalid'}]}))
            unknown = run(as_pane(self.a, FLEET_CONFIG=str(config)), '--host', 'nosuchhost.invalid', 'send', '--to', '%0', '--body', 'x')
            self.assertNotEqual(unknown.returncode, 0)
            self.assertIn('--from is required: no node for nosuchhost.invalid sets return_host', unknown.stderr)
        outside = subprocess.run([CLI, '--server', self.server, 'send', '--to', self.b, '--body', 'x'],
                                 env={k: v for k, v in os.environ.items() if k not in ('TMUX', 'TMUX_PANE')},
                                 text=True, capture_output=True, timeout=10)
        self.assertEqual(outside.stderr.strip(), 'fleet: --from is required outside tmux')

    def test_stale_pane_rejected(self):
        self.run_cli('send', '--to', '%999999', '--from', f'./{self.server}/{self.a}', '--body', 'hi', ok=False)
        self.run_cli('inbox', '--pane', '%999999', ok=False)

    def test_body_from_stdin_and_errors_go_to_stderr(self):
        p = subprocess.run([CLI, '--server', self.server, 'send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', '-'],
                           input='多行\n消息\n', text=True, capture_output=True, timeout=10)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(json.loads(self.run_cli('inbox', '--pane', self.b).stdout)[0]['body'], '多行\n消息\n')
        empty = self.run_cli('send', '--to', self.b, '--from', f'./{self.server}/{self.a}', '--body', '   ', ok=False)
        self.assertEqual(empty.stdout, '')
        self.assertEqual(empty.stderr.strip(), 'fleet: message body is empty')

    def test_without_a_terminal_bare_fleet_prints_help_rather_than_a_ui(self):
        p = subprocess.run([CLI], text=True, capture_output=True, timeout=10, stdin=subprocess.DEVNULL)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertIn('Usage:', p.stdout)
        self.assertNotIn('\x1b', p.stdout)

if __name__ == '__main__': unittest.main()
