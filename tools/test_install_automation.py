"""Exercise the real installer without loading a launchd job on the host."""
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile
import unittest


class InstallerTest(unittest.TestCase):
    def test_hook_checks_each_pushed_commit_and_blocks_on_failure(self):
        source = Path(__file__).resolve().parent.parent / ".githooks/pre-push"
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            fake = base / "make"
            fake.write_text('#!/bin/bash\nprintf "%s|%s\\n" "$*" "${VXD_PUBLIC_HISTORY_REFS:-}" >> "$VXD_TEST_GATE_LOG"\n'
                            'if [ "${VXD_PUBLIC_HISTORY_REFS:-}" = "${VXD_TEST_BAD_REF:-none}" ]; then exit 1; fi\n')
            fake.chmod(0o755)
            log = base / "gates.log"
            env = dict(os.environ, VXD_TEST_GATE_LOG=str(log), PATH=str(base) + os.pathsep + os.environ["PATH"])
            first, second, deleted = "1" * 40, "2" * 40, "0" * 40
            refs = f"refs/heads/main {first} refs/heads/main {deleted}\nrefs/heads/other {second} refs/heads/other {deleted}\nrefs/heads/removed {deleted} refs/heads/removed {first}\n"
            subprocess.run(["bash", str(source)], input=refs, text=True, env=env, check=True, capture_output=True)
            self.assertEqual(log.read_text().splitlines(), [f"public-history|{first}", f"public-history|{second}", "verify|"])
            log.unlink()
            env["VXD_TEST_BAD_REF"] = second
            failed = subprocess.run(["bash", str(source)], input=refs, text=True, env=env, capture_output=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(log.read_text().splitlines(), [f"public-history|{first}", f"public-history|{second}"])

    def test_render_install_reinstall_uninstall(self):
        source = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            repo = base / "repo & 'quoted' $(false)"
            home = base / "home & 'quoted' $(false)"
            commands = base / "bin"
            commands.mkdir()
            home.mkdir()
            shutil.copytree(source, repo / "tools")
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            fake = commands / "launchctl"
            fake.write_text('#!/bin/bash\nprintf "%s\\n" "$*" >> "$VXD_TEST_LAUNCHCTL_LOG"\n')
            fake.chmod(0o755)
            log = base / "launchctl.log"
            env = dict(os.environ, HOME=str(home), VXD_TEST_LAUNCHCTL_LOG=str(log),
                       PATH=str(commands) + os.pathsep + os.environ["PATH"])
            script = repo / "tools/install-automation.sh"
            destination = home / "Library/LaunchAgents/com.vxd.audit-loop.plist"
            for _ in range(2):
                subprocess.run(["bash", str(script)], env=env, check=True, capture_output=True)
                with destination.open("rb") as stream:
                    result = plistlib.load(stream)
                self.assertEqual(result["ProgramArguments"], ["/bin/bash", str(repo / "tools/audit-loop.sh")])
                self.assertEqual(result["WorkingDirectory"], str(repo))
                self.assertEqual(result["EnvironmentVariables"]["HOME"], str(home))
                self.assertEqual(result["StandardOutPath"], str(home / ".vxd/audit-loop/launchd.log"))
                self.assertNotIn("__VXD_", destination.read_text())
                self.assertEqual(subprocess.check_output(["git", "-C", str(repo), "config", "core.hooksPath"], env=env).strip(), b".githooks")
            self.assertEqual(log.read_text().count("\nload "), 2)
            subprocess.run(["bash", str(script), "--uninstall"], env=env, check=True, capture_output=True)
            self.assertFalse(destination.exists())
            self.assertNotEqual(subprocess.run(["git", "-C", str(repo), "config", "core.hooksPath"], env=env, capture_output=True).returncode, 0)


if __name__ == "__main__":
    unittest.main()
