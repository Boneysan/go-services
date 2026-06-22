import datetime
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from compare_saves import compute_streak, diff_rows


class DiffRowsTest(unittest.TestCase):
    def test_no_mismatch_when_fields_match(self):
        save_rows = [{"account_id": "1", "slot": 0, "name": "Zomevara", "race": "fyros", "gender": "male"}]
        db_rows = {("1", 0): {"name": "Zomevara", "race": "fyros", "gender": "male"}}
        self.assertEqual(diff_rows(save_rows, db_rows), [])

    def test_missing_db_row_is_a_mismatch(self):
        save_rows = [{"account_id": "1", "slot": 0, "name": "Zomevara", "race": "fyros", "gender": "male"}]
        mismatches = diff_rows(save_rows, {})
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0]["reason"], "missing_db_row")

    def test_field_mismatch_is_reported(self):
        save_rows = [{"account_id": "1", "slot": 0, "name": "Zomevara", "race": "fyros", "gender": "male"}]
        db_rows = {("1", 0): {"name": "Zomevara", "race": "matis", "gender": "male"}}
        mismatches = diff_rows(save_rows, db_rows)
        self.assertEqual(len(mismatches), 1)
        self.assertEqual(mismatches[0]["field"], "race")
        self.assertEqual(mismatches[0]["save"], "fyros")
        self.assertEqual(mismatches[0]["db"], "matis")


def d(s):
    return datetime.date.fromisoformat(s)


class ComputeStreakTest(unittest.TestCase):
    def test_empty_log_has_zero_streak(self):
        self.assertEqual(compute_streak([]), 0)

    def test_consecutive_clean_days_count(self):
        days = [(d("2026-06-21"), 0), (d("2026-06-20"), 0), (d("2026-06-19"), 0)]
        self.assertEqual(compute_streak(days), 3)

    def test_breaks_on_a_mismatch_day(self):
        days = [(d("2026-06-21"), 0), (d("2026-06-20"), 2), (d("2026-06-19"), 0)]
        self.assertEqual(compute_streak(days), 1)

    def test_breaks_on_a_gap_in_the_calendar(self):
        # 2026-06-19 is missing entirely (no run that day) -- streak stops
        # at the gap rather than silently skipping over it.
        days = [(d("2026-06-21"), 0), (d("2026-06-20"), 0), (d("2026-06-18"), 0)]
        self.assertEqual(compute_streak(days), 2)

    def test_most_recent_mismatch_gives_zero_streak(self):
        days = [(d("2026-06-21"), 1), (d("2026-06-20"), 0)]
        self.assertEqual(compute_streak(days), 0)


if __name__ == "__main__":
    unittest.main()
