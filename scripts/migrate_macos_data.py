#!/usr/bin/env python3
"""Move legacy local data into the macOS application directory without deleting it."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import stat
import subprocess
import sys
import tempfile


def require_stopped(target):
    result = subprocess.run(["ps", "-axo", "comm="], capture_output=True, text=True, check=True)
    runtime = str(target / "runtime" / "camoufox") + os.sep
    for line in result.stdout.splitlines():
        executable = line.strip()
        if Path(executable).name == "tulai" or executable.startswith(runtime):
            raise RuntimeError("请先退出 tulai 并关闭其内置浏览器")


def tree_manifest(root):
    result = {}
    for base, dirs, files in os.walk(root, followlinks=False):
        for name in dirs + files:
            path = Path(base) / name
            relative = str(path.relative_to(root))
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode):
                result[relative] = ("link", os.readlink(path))
            elif stat.S_ISDIR(mode):
                result[relative] = ("dir", stat.S_IMODE(mode))
            elif stat.S_ISREG(mode):
                digest = hashlib.sha256()
                with path.open("rb") as stream:
                    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                        digest.update(chunk)
                result[relative] = ("file", stat.S_IMODE(mode), digest.hexdigest())
            else:
                raise RuntimeError(f"无法安全复制特殊文件: {path}")
    return result


def copy_verified(source, target):
    shutil.copytree(source, target, symlinks=True, copy_function=shutil.copy2)
    if tree_manifest(source) != tree_manifest(target):
        raise RuntimeError(f"复制校验失败，原数据未更改: {source}")


def relocated_path(value, old, new):
    if not isinstance(value, str) or not os.path.isabs(value):
        return value
    try:
        relative = Path(value).relative_to(old)
    except ValueError:
        try:
            relative = Path(value).resolve().relative_to(old.resolve())
        except ValueError:
            return value
    return str(new / relative)


def relocate_json(value, old, new):
    if isinstance(value, list):
        return [relocate_json(item, old, new) for item in value]
    if isinstance(value, dict):
        return {key: relocate_json(item, old, new) for key, item in value.items()}
    return relocated_path(value, old, new)


def rewrite_database(db, source, target, move_output):
    profile_root = source / "BrowserData"
    columns = {row[1] for row in db.execute("PRAGMA table_info(browsers)")}
    rows = db.execute("SELECT id, platform, user_data_dir, legacy_user_data_dir, is_logged_in, COALESCE(ext_account,'') FROM browsers").fetchall() if "legacy_user_data_dir" in columns else ()
    for account_id, platform, current, legacy, logged_in, ext_account in rows:
        if platform not in ("豆包", "千问") or ext_account:
            continue
        if current:
            path = Path(current) if os.path.isabs(current) else source / current
            try:
                relative = path.resolve().relative_to(profile_root.resolve())
            except ValueError as error:
                raise RuntimeError(f"账号 {account_id} 的资料不在原浏览器目录内，未迁移") from error
            if logged_in and not path.is_dir():
                raise RuntimeError(f"账号 {account_id} 的登录资料目录不存在，未迁移")
            if path.is_dir():
                for base, dirs, files in os.walk(path, followlinks=False):
                    for name in dirs + files:
                        entry = Path(base) / name
                        if entry.is_symlink():
                            try:
                                entry.resolve().relative_to(profile_root.resolve())
                            except ValueError as error:
                                raise RuntimeError(f"账号 {account_id} 的资料链接指向浏览器目录外，未迁移") from error
            current = str(Path("BrowserData") / relative)
        elif logged_in and not (profile_root / "Camoufox" / f"account-{account_id}").is_dir():
            raise RuntimeError(f"账号 {account_id} 的登录资料目录不存在，未迁移")
        if legacy and os.path.isabs(legacy):
            legacy = relocated_path(legacy, profile_root, Path("BrowserData"))
        db.execute("UPDATE browsers SET user_data_dir=?, legacy_user_data_dir=? WHERE id=?", (current, legacy, account_id))

    if not move_output:
        row = db.execute("SELECT value FROM settings WHERE key='output_path'").fetchone()
        selected = row[0].strip() if row and row[0] else ""
        if selected and not os.path.isabs(selected):
            db.execute("UPDATE settings SET value=? WHERE key='output_path'", (str((source / selected).resolve()),))
        return
    old = source / "输出结果"
    new = target / "data" / "输出结果"
    db.execute("INSERT INTO settings(key,value) VALUES('output_path',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", (str(new),))
    for key in ("source_images", "wash_images"):
        row = db.execute("SELECT value FROM settings WHERE key=?", (key,)).fetchone()
        if row and row[0]:
            db.execute("UPDATE settings SET value=? WHERE key=?", ("\n".join(relocated_path(p, old, new) for p in row[0].split("\n")), key))
    row = db.execute("SELECT value FROM settings WHERE key='download_records'").fetchone()
    if row and row[0]:
        try:
            records = json.loads(row[0])
        except json.JSONDecodeError as error:
            if str(old) in row[0]:
                raise RuntimeError("下载历史无法解析，未迁移") from error
        else:
            db.execute("UPDATE settings SET value=? WHERE key='download_records'", (json.dumps(relocate_json(records, old, new), ensure_ascii=False),))
    tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    for table, columns in (("sessions", ("images",)), ("session_tasks", ("source", "outputs"))):
        if table not in tables:
            continue
        for column in columns:
            for row_id, value in db.execute(f"SELECT id, {column} FROM {table} WHERE {column}<>''").fetchall():
                rewritten = "\n".join(relocated_path(p, old, new) for p in value.split("\n"))
                if rewritten != value:
                    db.execute(f"UPDATE {table} SET {column}=? WHERE id=?", (rewritten, row_id))


def migrate(source, target):
    source, target = Path(source).resolve(), Path(target).resolve()
    database, profiles = source / "database", source / "BrowserData"
    if not (database / "config.db").is_file() or ((profiles.exists() or profiles.is_symlink()) and not profiles.is_dir()):
        raise RuntimeError("找不到原账号数据库或浏览器资料目录无效")
    target.mkdir(parents=True, exist_ok=True, mode=0o700)
    data = target / "data"
    if data.exists() or data.is_symlink():
        if all((target / name).is_symlink() and (target / name).resolve() == (data / name).resolve() for name in ("database", "BrowserData")):
            return False
        raise RuntimeError(f"目标目录已存在，未覆盖: {data}")
    previous = {}
    for name in ("database", "BrowserData"):
        path = target / name
        if path.is_symlink() and path.resolve() == (source / name).resolve():
            previous[name] = os.readlink(path)
        elif os.path.lexists(path):
            raise RuntimeError(f"应用已有独立的数据目录，未覆盖: {path}")
        else:
            previous[name] = None
    require_stopped(target)
    lock = sqlite3.connect(database / "config.db", timeout=0.1)
    stage = None
    switched = []
    try:
        lock.execute("BEGIN IMMEDIATE")
        if lock.execute("PRAGMA quick_check").fetchone() != ("ok",):
            raise RuntimeError("原数据库完整性检查失败")
        columns = {row[1] for row in lock.execute("PRAGMA table_info(browsers)")}
        if not {"id", "platform", "name", "port", "user_data_dir", "is_running", "is_logged_in"} <= columns:
            raise RuntimeError("原账号数据库结构无效")
        fields = [name for name in ("id", "platform", "name", "is_logged_in", "health", "day_key", "day_count", "day_success", "day_failed") if name in columns]
        snapshot_sql = f"SELECT {','.join(fields)} FROM browsers ORDER BY id"
        before = lock.execute(snapshot_sql).fetchall()
        stage = Path(tempfile.mkdtemp(prefix=".migration-", dir=target))
        (stage / "database").mkdir(mode=0o700)
        readonly = sqlite3.connect((database / "config.db").as_uri() + "?mode=ro", uri=True)
        dest = sqlite3.connect(stage / "database" / "config.db")
        try:
            readonly.backup(dest)
            dest.commit()
        finally:
            dest.close()
            readonly.close()
        print("正在复制并校验浏览器资料…", flush=True)
        if profiles.is_dir():
            copy_verified(profiles, stage / "BrowserData")
        else:
            (stage / "BrowserData").mkdir(mode=0o700)
        output = source / "输出结果"
        row = lock.execute("SELECT value FROM settings WHERE key='output_path'").fetchone()
        selected = row[0].strip() if row and row[0] else ""
        selected_path = Path(selected) if os.path.isabs(selected) else source / selected
        move_output = (selected_path.resolve() == output.resolve()) if selected else output.exists()
        if move_output:
            print("正在复制并校验默认输出目录…", flush=True)
            if output.is_dir():
                copy_verified(output, stage / "输出结果")
            elif output.exists():
                raise RuntimeError("默认输出路径不是目录，未迁移")
            else:
                (stage / "输出结果").mkdir(mode=0o700)
        dest = sqlite3.connect(stage / "database" / "config.db")
        try:
            rewrite_database(dest, source, target, move_output)
            dest.commit()
            after = dest.execute(snapshot_sql).fetchall()
            if after != before or dest.execute("PRAGMA quick_check").fetchone() != ("ok",):
                raise RuntimeError("账号记录或新数据库校验失败")
        finally:
            dest.close()
        require_stopped(target)
        for name in ("database", "BrowserData"):
            path = target / name
            if previous[name] is None:
                if os.path.lexists(path):
                    raise RuntimeError(f"目标目录在迁移时发生变化: {path}")
            elif not path.is_symlink() or os.readlink(path) != previous[name]:
                raise RuntimeError(f"目标链接在迁移时发生变化: {path}")
        os.rename(stage, data)
        stage = None
        for name in ("BrowserData", "database"):
            temp = target / f".migration-link-{name}"
            os.symlink(str(Path("data") / name), temp)
            try:
                path = target / name
                if previous[name] is None and os.path.lexists(path):
                    raise RuntimeError(f"目标目录在迁移时发生变化: {path}")
                if previous[name] is not None and (not path.is_symlink() or os.readlink(path) != previous[name]):
                    raise RuntimeError(f"目标链接在迁移时发生变化: {path}")
                os.replace(temp, target / name)
            finally:
                if temp.is_symlink():
                    temp.unlink()
            switched.append(name)
        return True
    except BaseException:
        for name in reversed(switched):
            path = target / name
            if path.is_symlink() and os.readlink(path) == str(Path("data") / name):
                if previous[name] is None:
                    path.unlink()
                else:
                    temp = target / f".migration-restore-{name}"
                    os.symlink(previous[name], temp)
                    os.replace(temp, path)
        raise
    finally:
        lock.rollback()
        lock.close()
        if stage and stage.exists():
            shutil.rmtree(stage)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path.cwd())
    parser.add_argument("--target", type=Path, default=Path.home() / "Library/Application Support/tulai")
    args = parser.parse_args()
    try:
        changed = migrate(args.source, args.target)
    except (OSError, sqlite3.Error, RuntimeError, subprocess.SubprocessError) as error:
        print(f"迁移未完成，原数据保留: {error}", file=sys.stderr)
        return 1
    print("迁移完成；原项目数据仍保留为备份。" if changed else "应用数据已迁移。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
