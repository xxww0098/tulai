import json
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest import mock

import migrate_macos_data as migration


class MigrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.source = Path(self.tmp.name).resolve() / "project"
        self.target = Path(self.tmp.name).resolve() / "app data"
        self.target.mkdir()
        (self.source / "database").mkdir(parents=True)
        profile = self.source / "BrowserData/Camoufox/account-1"
        profile.mkdir(parents=True)
        (profile / "cookies.sqlite").write_bytes(b"saved login")
        (profile / ".tulai-identity.json").write_bytes(b"saved fingerprint")
        output = self.source / "输出结果"
        output.mkdir()
        (output / "result.png").write_bytes(b"existing result")
        db = sqlite3.connect(self.source / "database/config.db")
        db.executescript("""
            CREATE TABLE browsers (id INTEGER PRIMARY KEY, platform TEXT, name TEXT, user_data_dir TEXT,
                legacy_user_data_dir TEXT, is_logged_in INTEGER, ext_account TEXT, health TEXT,
                day_key TEXT, day_count INTEGER, day_success INTEGER, day_failed INTEGER);
            CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
            CREATE TABLE sessions (id INTEGER PRIMARY KEY, images TEXT);
            CREATE TABLE session_tasks (id INTEGER PRIMARY KEY, source TEXT, outputs TEXT);
        """)
        db.execute("INSERT INTO browsers VALUES (1,'豆包','account',?, '',1,'','ready','2026-11-03',47,43,14)", (str(profile),))
        db.execute("INSERT INTO settings VALUES ('output_path',?)", (str(output),))
        db.execute("INSERT INTO settings VALUES ('download_records',?)", (json.dumps([{"path": str(output / "result.png")}]),))
        db.execute("INSERT INTO sessions VALUES (1,?)", (str(output / "result.png"),))
        db.execute("INSERT INTO session_tasks VALUES (1,'',?)", (str(output / "result.png"),))
        db.commit()
        db.close()
        for name in ("database", "BrowserData"):
            os.symlink(self.source / name, self.target / name)

    def test_migrates_accounts_profiles_and_default_output(self):
        with mock.patch.object(migration, "require_stopped"):
            self.assertTrue(migration.migrate(self.source, self.target))
            self.assertFalse(migration.migrate(self.source, self.target))
        for name in ("database", "BrowserData"):
            self.assertEqual((self.target / name).resolve(), self.target / "data" / name)
            self.assertFalse((self.target / "data" / name).is_symlink())
        profile = self.target / "BrowserData/Camoufox/account-1"
        self.assertEqual((profile / "cookies.sqlite").read_bytes(), b"saved login")
        self.assertEqual((profile / ".tulai-identity.json").read_bytes(), b"saved fingerprint")
        self.assertEqual((self.source / "BrowserData/Camoufox/account-1/cookies.sqlite").read_bytes(), b"saved login")
        db = sqlite3.connect(self.target / "database/config.db")
        account = db.execute("SELECT user_data_dir,is_logged_in,health,day_count,day_success,day_failed FROM browsers").fetchone()
        self.assertEqual(account, ("BrowserData/Camoufox/account-1", 1, "ready", 47, 43, 14))
        expected = str(self.target / "data/输出结果/result.png")
        self.assertEqual(db.execute("SELECT outputs FROM session_tasks").fetchone()[0], expected)
        self.assertEqual(db.execute("SELECT images FROM sessions").fetchone()[0], expected)
        records = json.loads(db.execute("SELECT value FROM settings WHERE key='download_records'").fetchone()[0])
        self.assertEqual(records[0]["path"], expected)
        db.close()
        self.assertEqual(Path(expected).read_bytes(), b"existing result")

    def test_never_overwrites_independent_app_data(self):
        (self.target / "database").unlink()
        (self.target / "database").mkdir()
        (self.target / "database/config.db").write_bytes(b"other data")
        with mock.patch.object(migration, "require_stopped"):
            with self.assertRaisesRegex(RuntimeError, "未覆盖"):
                migration.migrate(self.source, self.target)
        self.assertEqual((self.target / "database/config.db").read_bytes(), b"other data")
        self.assertEqual((self.target / "BrowserData").resolve(), self.source / "BrowserData")

    def test_failure_during_switch_restores_old_links(self):
        replace = os.replace

        def fail_database(src, dst):
            if Path(dst) == self.target / "database" and Path(src).name == ".migration-link-database":
                raise OSError("simulated failure")
            return replace(src, dst)

        with mock.patch.object(migration, "require_stopped"), mock.patch.object(migration.os, "replace", side_effect=fail_database):
            with self.assertRaisesRegex(OSError, "simulated failure"):
                migration.migrate(self.source, self.target)
        for name in ("database", "BrowserData"):
            self.assertEqual((self.target / name).resolve(), self.source / name)
        self.assertTrue((self.target / "data/database/config.db").is_file())

    def test_rejects_profile_outside_source(self):
        db = sqlite3.connect(self.source / "database/config.db")
        db.execute("UPDATE browsers SET user_data_dir=?", (str(self.target),))
        db.commit()
        db.close()
        with mock.patch.object(migration, "require_stopped"):
            with self.assertRaisesRegex(RuntimeError, "资料不在"):
                migration.migrate(self.source, self.target)
        self.assertEqual((self.target / "database").resolve(), self.source / "database")
        self.assertFalse((self.target / "data").exists())

    def test_rejects_active_app(self):
        with mock.patch.object(migration, "require_stopped", side_effect=RuntimeError("app running")):
            with self.assertRaisesRegex(RuntimeError, "app running"):
                migration.migrate(self.source, self.target)
        self.assertEqual((self.target / "database").resolve(), self.source / "database")
        self.assertFalse((self.target / "data").exists())

    def test_output_paths_through_project_alias_are_relocated(self):
        alias = self.source.parent / "project alias"
        os.symlink(self.source, alias)
        old_output = alias / "输出结果"
        db = sqlite3.connect(self.source / "database/config.db")
        db.execute("UPDATE settings SET value=? WHERE key='output_path'", (str(old_output),))
        db.execute("UPDATE session_tasks SET outputs=?", (str(old_output / "result.png"),))
        db.commit()
        db.close()
        with mock.patch.object(migration, "require_stopped"):
            self.assertTrue(migration.migrate(alias, self.target))
        db = sqlite3.connect(self.target / "database/config.db")
        self.assertEqual(db.execute("SELECT outputs FROM session_tasks").fetchone()[0], str(self.target / "data/输出结果/result.png"))
        self.assertEqual(db.execute("SELECT value FROM settings WHERE key='output_path'").fetchone()[0], str(self.target / "data/输出结果"))
        db.close()


if __name__ == "__main__":
    unittest.main()
