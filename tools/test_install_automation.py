"""Exercise the real installer without loading a launchd job on the host."""
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile
import unittest


class InstallerTest(unittest.TestCase):
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
