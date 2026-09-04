#!/usr/bin/env python3
"""公開樹 gate：Git 追蹤的檔案裡不得有 ROM、BIOS、savestate、trace 或任何 oracle 產物。

repo 是 public（docs/PLAN.md §十一），這裡是 `.gitignore` 之外的第二層。
"""

from __future__ import annotations

import subprocess
import unittest
from pathlib import Path

PRIVATE_SUFFIXES = {
    ".pce", ".zip", ".rom", ".bin", ".cue", ".iso", ".sav", ".srm", ".state", ".mss",
    ".bios", ".vgm", ".wav", ".ogg", ".mp4",
}
PRIVATE_PARTS = {"private", "fixtures-private", "dist-all", "oracle-output", "rom-dumps"}
# 單一原始檔的合理上限：任何比這大的追蹤檔案都要有人說得出來歷。
MAX_TRACKED_BYTES = 512 * 1024


class PublicTreeTests(unittest.TestCase):
    def test_tracked_tree_excludes_private_inputs(self) -> None:
        root = Path(__file__).resolve().parents[1]
        result = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z"], check=True, capture_output=True
        )
        tracked = [Path(name) for name in result.stdout.decode("utf-8").split("\0") if name]
        self.assertTrue(tracked, "git ls-files 回空：不是 git repo 或沒有追蹤檔案")
        offenders = []
        for path in tracked:
            if path.suffix.lower() in PRIVATE_SUFFIXES:
                offenders.append(f"{path}: 私人副檔名")
            if PRIVATE_PARTS & set(part.lower() for part in path.parts):
                offenders.append(f"{path}: 私人目錄")
            full = root / path
            if full.is_file() and full.stat().st_size > MAX_TRACKED_BYTES:
                offenders.append(f"{path}: {full.stat().st_size} bytes 超過上限")
        self.assertEqual([], offenders)

    def test_license_and_notice_present(self) -> None:
        root = Path(__file__).resolve().parents[1]
        license_text = (root / "LICENSE").read_text(encoding="utf-8")
        self.assertIn("RRSAL-1.0", license_text)
        self.assertNotIn("@PROJECT_ZH@", license_text)
        readme = (root / "README.md").read_text(encoding="utf-8")
        self.assertIn("RRSAL-1.0", readme)


if __name__ == "__main__":
    unittest.main()
