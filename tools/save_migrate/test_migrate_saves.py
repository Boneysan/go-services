import unittest
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from migrate_saves import find_field


class FindFieldTest(unittest.TestCase):
    def test_finds_root_field(self):
        root = [{"n": "_Name", "t": "STRING", "v": "Synthetic"}]
        self.assertEqual(find_field(root, "_Name"), "Synthetic")

    def test_finds_nested_entity_base_field(self):
        root = [
            {"n": "VERSION", "t": "UINT32", "v": "25"},
            {"n": "EntityBase", "c": [
                {"n": "_Name", "t": "STRING", "v": "Zomevara"},
                {"n": "_Race", "t": "STRING", "v": "Fyros"},
                {"n": "_Gender", "t": "UINT32", "v": "0"},
            ]},
        ]
        self.assertEqual(find_field(root, "_Name"), "Zomevara")
        self.assertEqual(find_field(root, "_Race"), "Fyros")
        self.assertEqual(find_field(root, "_Gender"), "0")


if __name__ == "__main__":
    unittest.main()
